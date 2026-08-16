package problem

import (
	"context"
	"strings"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

const (
	StatusInReview         = "in_review"
	StatusChangesRequested = "changes_requested"

	ReviewDecisionApprove        = "approve"
	ReviewDecisionRequestChanges = "request_changes"
)

type ProblemReviewEvent struct {
	ID          int64     `json:"id"`
	ProblemID   int64     `json:"problem_id"`
	ActorUserID int64     `json:"actor_user_id"`
	FromStatus  string    `json:"from_status"`
	ToStatus    string    `json:"to_status"`
	Decision    string    `json:"decision"`
	Comment     string    `json:"comment,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

type ProblemReviewDecisionInput struct {
	Decision string `json:"decision"`
	Comment  string `json:"comment"`
}

type ProblemReviewQueueFilter struct {
	Page     int32
	PageSize int32
}

type ProblemReviewQueue struct {
	Items    []ProblemResponse `json:"items"`
	Total    int64             `json:"total"`
	Page     int32             `json:"page"`
	PageSize int32             `json:"page_size"`
}

type problemReviewStore interface {
	GetProblem(context.Context, int64) (ProblemRecord, error)
	ListProblemsForReview(context.Context, ProblemReviewQueueFilter) ([]ProblemRecord, int64, error)
	ListProblemReviewEvents(context.Context, int64) ([]ProblemReviewEvent, error)
	WithProblemReviewTx(context.Context, func(context.Context, problemReviewTx) error) error
}

type problemReviewTx interface {
	LockProblemForUpdate(context.Context, int64) (ProblemRecord, error)
	SetProblemStatus(context.Context, int64, string) (ProblemRecord, error)
	CreateProblemReviewEvent(context.Context, ProblemReviewEvent) (ProblemReviewEvent, error)
}

type problemReviewReadiness interface {
	problemPublishReadStore
}

type ProblemReviewPolicy interface {
	CanSubmitReview(auth.Actor, ProblemRecord) error
	CanDecideReview(auth.Actor, ProblemRecord) error
	CanViewReviewQueue(auth.Actor) error
	CanViewReviewEvents(auth.Actor, ProblemRecord) error
}

type ProblemReviewService struct {
	store     problemReviewStore
	readiness problemReviewReadiness
	policy    ProblemReviewPolicy
}

func NewProblemReviewService(store problemReviewStore, readiness problemReviewReadiness, policy ProblemReviewPolicy) *ProblemReviewService {
	if store == nil {
		panic("problem review store is required")
	}
	if readiness == nil {
		panic("problem review readiness is required")
	}
	if policy == nil {
		panic("problem review policy is required")
	}
	return &ProblemReviewService{store: store, readiness: readiness, policy: policy}
}

func (s *ProblemReviewService) Submit(ctx context.Context, actor auth.Actor, problemID int64) (ProblemRecord, error) {
	problem, err := s.store.GetProblem(ctx, problemID)
	if err != nil {
		return ProblemRecord{}, err
	}
	if err := s.policy.CanSubmitReview(actor, problem); err != nil {
		return ProblemRecord{}, err
	}
	if problem.Status != StatusDraft && problem.Status != StatusChangesRequested {
		return ProblemRecord{}, apperror.Conflict("problem.review_invalid_state", "only draft or changes-requested problems can be submitted for review")
	}
	return s.transition(ctx, actor, problemID, problem.Status, StatusInReview, "submit", "")
}

func (s *ProblemReviewService) Decide(ctx context.Context, actor auth.Actor, problemID int64, input ProblemReviewDecisionInput) (ProblemRecord, error) {
	input.Decision = strings.TrimSpace(input.Decision)
	input.Comment = strings.TrimSpace(input.Comment)
	if input.Decision != ReviewDecisionApprove && input.Decision != ReviewDecisionRequestChanges {
		return ProblemRecord{}, apperror.BadRequest("problem.review_decision_invalid", "decision must be approve or request_changes")
	}
	if input.Decision == ReviewDecisionRequestChanges && input.Comment == "" {
		return ProblemRecord{}, apperror.BadRequest("problem.review_comment_required", "comment is required when requesting changes")
	}

	problem, err := s.store.GetProblem(ctx, problemID)
	if err != nil {
		return ProblemRecord{}, err
	}
	if err := s.policy.CanDecideReview(actor, problem); err != nil {
		return ProblemRecord{}, err
	}
	if problem.Status != StatusInReview {
		return ProblemRecord{}, apperror.Conflict("problem.review_invalid_state", "only problems in review can be decided")
	}
	if input.Decision == ReviewDecisionApprove {
		if err := ensurePublishable(ctx, s.readiness, problemID); err != nil {
			return ProblemRecord{}, err
		}
		return s.transition(ctx, actor, problemID, problem.Status, StatusPublished, input.Decision, input.Comment)
	}
	return s.transition(ctx, actor, problemID, problem.Status, StatusChangesRequested, input.Decision, input.Comment)
}

func (s *ProblemReviewService) Queue(ctx context.Context, actor auth.Actor, filter ProblemReviewQueueFilter) (ProblemReviewQueue, error) {
	if err := s.policy.CanViewReviewQueue(actor); err != nil {
		return ProblemReviewQueue{}, err
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	items, total, err := s.store.ListProblemsForReview(ctx, filter)
	if err != nil {
		return ProblemReviewQueue{}, err
	}
	responses := make([]ProblemResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, problemResponseWithoutStore(item))
	}
	return ProblemReviewQueue{Items: responses, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (s *ProblemReviewService) Events(ctx context.Context, actor auth.Actor, problemID int64) ([]ProblemReviewEvent, error) {
	problem, err := s.store.GetProblem(ctx, problemID)
	if err != nil {
		return nil, err
	}
	if err := s.policy.CanViewReviewEvents(actor, problem); err != nil {
		return nil, err
	}
	return s.store.ListProblemReviewEvents(ctx, problemID)
}

func (s *ProblemReviewService) transition(ctx context.Context, actor auth.Actor, problemID int64, fromStatus, toStatus, decision, comment string) (ProblemRecord, error) {
	var updated ProblemRecord
	err := s.store.WithProblemReviewTx(ctx, func(ctx context.Context, tx problemReviewTx) error {
		current, err := tx.LockProblemForUpdate(ctx, problemID)
		if err != nil {
			return err
		}
		if current.Status != fromStatus {
			return apperror.Conflict("problem.review_invalid_state", "problem state changed before the review action completed")
		}
		updated, err = tx.SetProblemStatus(ctx, problemID, toStatus)
		if err != nil {
			return err
		}
		_, err = tx.CreateProblemReviewEvent(ctx, ProblemReviewEvent{
			ProblemID:   problemID,
			ActorUserID: actor.UserID,
			FromStatus:  current.Status,
			ToStatus:    toStatus,
			Decision:    decision,
			Comment:     comment,
		})
		return err
	})
	return updated, err
}

func problemResponseWithoutStore(p ProblemRecord) ProblemResponse {
	return ProblemResponse{
		ID:          p.ID,
		Title:       p.Title,
		Slug:        p.Slug,
		Difficulty:  p.Difficulty,
		Visibility:  p.Visibility,
		Status:      p.Status,
		OwnerUserID: p.OwnerUserID,
		Limits: ProblemLimits{
			TimeLimitMS:   p.TimeLimitMS,
			MemoryLimitKB: p.MemoryLimitKB,
		},
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
		PublishedAt: p.PublishedAt,
		Tags:        []string{},
	}
}
