package submission

import (
	"context"
	"errors"
	"strings"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

const (
	RejudgeBatchStatusQueued    = "queued"
	RejudgeBatchStatusRunning   = "running"
	RejudgeBatchStatusCompleted = "completed"
	RejudgeBatchStatusFailed    = "failed"
	RejudgeBatchStatusCanceled  = "canceled"

	RejudgeItemStatusQueued    = "queued"
	RejudgeItemStatusRunning   = "running"
	RejudgeItemStatusCompleted = "completed"
	RejudgeItemStatusFailed    = "failed"
	RejudgeItemStatusCanceled  = "canceled"
)

var ErrNoRejudgeSubmissions = errors.New("no eligible submissions for rejudge")

type RejudgeBatchRecord struct {
	ID             int64      `json:"id"`
	ProblemID      *int64     `json:"problem_id,omitempty"`
	ContestID      *int64     `json:"contest_id,omitempty"`
	RequestedBy    int64      `json:"requested_by"`
	Status         string     `json:"status"`
	Reason         string     `json:"reason"`
	TotalCount     int32      `json:"total_count"`
	CompletedCount int32      `json:"completed_count"`
	FailedCount    int32      `json:"failed_count"`
	CanceledCount  int32      `json:"canceled_count"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type RejudgeBatchItemRecord struct {
	ID           int64      `json:"id"`
	BatchID      int64      `json:"batch_id"`
	SubmissionID int64      `json:"submission_id"`
	TaskID       int64      `json:"task_id"`
	AttemptID    *int64     `json:"attempt_id,omitempty"`
	Status       string     `json:"status"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type CreateRejudgeBatchInput struct {
	ProblemID *int64 `json:"problem_id"`
	ContestID *int64 `json:"contest_id"`
	Reason    string `json:"reason"`
}

type CreateRejudgeBatchRecordInput struct {
	ProblemID   *int64
	ContestID   *int64
	RequestedBy int64
	Reason      string
	NextRunAt   time.Time
}

type ListRejudgeBatchesInput struct {
	ProblemID   *int64
	ContestID   *int64
	RequestedBy *int64
	Status      *string
	Offset      int32
	Limit       int32
}

type RejudgeBatchDetail struct {
	Batch RejudgeBatchRecord       `json:"batch"`
	Items []RejudgeBatchItemRecord `json:"items"`
}

type RejudgeRepository interface {
	CreateRejudgeBatchWithItems(context.Context, CreateRejudgeBatchRecordInput) (RejudgeBatchRecord, error)
	GetRejudgeBatch(context.Context, int64) (RejudgeBatchRecord, error)
	ListRejudgeBatches(context.Context, ListRejudgeBatchesInput) ([]RejudgeBatchRecord, int64, error)
	ListRejudgeBatchItems(context.Context, int64) ([]RejudgeBatchItemRecord, error)
	CancelRejudgeBatch(context.Context, int64, string) (RejudgeBatchRecord, error)
}

type RejudgePolicy interface {
	AuthorizeProblemRejudge(context.Context, auth.Actor, int64) error
	AuthorizeContestRejudge(context.Context, auth.Actor, int64) error
	ValidateContestRejudgeTarget(context.Context, int64) error
}

type RejudgeMetrics interface {
	RecordRejudgeBatch(action, target, result string)
}

type RejudgeService struct {
	repo    RejudgeRepository
	policy  RejudgePolicy
	now     func() time.Time
	metrics RejudgeMetrics
}

func NewRejudgeService(repo RejudgeRepository, policy RejudgePolicy, now func() time.Time, metrics ...RejudgeMetrics) *RejudgeService {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	service := &RejudgeService{repo: repo, policy: policy, now: now}
	if len(metrics) > 0 {
		service.metrics = metrics[0]
	}
	return service
}

func (s *RejudgeService) CreateBatch(ctx context.Context, actor auth.Actor, input CreateRejudgeBatchInput) (RejudgeBatchRecord, error) {
	if !actor.Authenticated() {
		return RejudgeBatchRecord{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	if (input.ProblemID == nil) == (input.ContestID == nil) {
		return RejudgeBatchRecord{}, apperror.BadRequest("rejudge.target_invalid", "exactly one rejudge target is required")
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return RejudgeBatchRecord{}, apperror.BadRequest("rejudge.reason_required", "rejudge reason is required")
	}
	if len(reason) > 500 {
		return RejudgeBatchRecord{}, apperror.BadRequest("rejudge.reason_invalid", "rejudge reason must not exceed 500 bytes")
	}
	if err := s.authorize(ctx, actor, input.ProblemID, input.ContestID); err != nil {
		return RejudgeBatchRecord{}, err
	}
	if input.ContestID != nil {
		if err := s.policy.ValidateContestRejudgeTarget(ctx, *input.ContestID); err != nil {
			return RejudgeBatchRecord{}, err
		}
	}
	batch, err := s.repo.CreateRejudgeBatchWithItems(ctx, CreateRejudgeBatchRecordInput{
		ProblemID: input.ProblemID, ContestID: input.ContestID, RequestedBy: actor.UserID, Reason: reason, NextRunAt: s.now().UTC(),
	})
	if errors.Is(err, ErrNoRejudgeSubmissions) {
		s.record("create", rejudgeTargetType(input.ProblemID), "error")
		return RejudgeBatchRecord{}, apperror.Unprocessable("rejudge.no_submissions", "no eligible terminal submissions found")
	}
	if err != nil {
		s.record("create", rejudgeTargetType(input.ProblemID), "error")
		return RejudgeBatchRecord{}, err
	}
	s.record("create", rejudgeTargetType(input.ProblemID), "success")
	return batch, err
}

func (s *RejudgeService) GetBatch(ctx context.Context, actor auth.Actor, id int64) (RejudgeBatchDetail, error) {
	if !actor.Authenticated() {
		return RejudgeBatchDetail{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	batch, err := s.repo.GetRejudgeBatch(ctx, id)
	if err != nil {
		return RejudgeBatchDetail{}, err
	}
	if err := s.authorize(ctx, actor, batch.ProblemID, batch.ContestID); err != nil {
		return RejudgeBatchDetail{}, err
	}
	items, err := s.repo.ListRejudgeBatchItems(ctx, id)
	if err != nil {
		return RejudgeBatchDetail{}, err
	}
	return RejudgeBatchDetail{Batch: batch, Items: items}, nil
}

func (s *RejudgeService) ListBatches(ctx context.Context, actor auth.Actor, input ListRejudgeBatchesInput) ([]RejudgeBatchRecord, int64, error) {
	if !actor.Authenticated() {
		return nil, 0, apperror.Unauthorized("auth_required", "authentication required")
	}
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 50
	}
	if input.Offset < 0 {
		input.Offset = 0
	}
	if !actor.Admin() {
		input.RequestedBy = &actor.UserID
	}
	return s.repo.ListRejudgeBatches(ctx, input)
}

func (s *RejudgeService) CancelBatch(ctx context.Context, actor auth.Actor, id int64, reason string) (RejudgeBatchRecord, error) {
	if !actor.Authenticated() {
		return RejudgeBatchRecord{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return RejudgeBatchRecord{}, apperror.BadRequest("rejudge.cancel_reason_required", "cancellation reason is required")
	}
	if len(reason) > 500 {
		return RejudgeBatchRecord{}, apperror.BadRequest("rejudge.reason_invalid", "cancellation reason must not exceed 500 bytes")
	}
	batch, err := s.repo.GetRejudgeBatch(ctx, id)
	if err != nil {
		return RejudgeBatchRecord{}, err
	}
	if err := s.authorize(ctx, actor, batch.ProblemID, batch.ContestID); err != nil {
		return RejudgeBatchRecord{}, err
	}
	canceled, err := s.repo.CancelRejudgeBatch(ctx, id, reason)
	if err != nil {
		s.record("cancel", rejudgeTargetType(batch.ProblemID), "error")
		return RejudgeBatchRecord{}, err
	}
	s.record("cancel", rejudgeTargetType(batch.ProblemID), "success")
	return canceled, nil
}

func (s *RejudgeService) record(action, target, result string) {
	if s.metrics != nil {
		s.metrics.RecordRejudgeBatch(action, target, result)
	}
}

func rejudgeTargetType(problemID *int64) string {
	if problemID != nil {
		return "problem"
	}
	return "contest"
}

func (s *RejudgeService) authorize(ctx context.Context, actor auth.Actor, problemID, contestID *int64) error {
	if problemID != nil {
		return s.policy.AuthorizeProblemRejudge(ctx, actor, *problemID)
	}
	if contestID != nil {
		return s.policy.AuthorizeContestRejudge(ctx, actor, *contestID)
	}
	return apperror.BadRequest("rejudge.target_invalid", "exactly one rejudge target is required")
}
