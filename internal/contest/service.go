package contest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/submission"
)

const (
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"

	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusRunning   = "running"
	StatusEnded     = "ended"
	StatusArchived  = "archived"

	RegistrationActive   = "active"
	RegistrationCanceled = "canceled"

	ScoringModeACM = "acm"

	CellNone      = "none"
	CellAttempted = "attempted"
	CellAccepted  = "accepted"
	CellFrozen    = "frozen"
)

type ScoreboardView string

const (
	ScoreboardViewLive   ScoreboardView = "live"
	ScoreboardViewFrozen ScoreboardView = "frozen"
	ScoreboardViewFinal  ScoreboardView = "final"
)

type ContestRecord struct {
	ID             int64            `json:"id"`
	OwnerUserID    int64            `json:"owner_user_id"`
	Title          string           `json:"title"`
	Description    *string          `json:"description,omitempty"`
	Visibility     string           `json:"visibility"`
	Status         string           `json:"status"`
	StartAt        time.Time        `json:"start_at"`
	EndAt          time.Time        `json:"end_at"`
	FreezeAt       time.Time        `json:"freeze_at"`
	InviteCodeHash string           `json:"-"`
	ScoringMode    string           `json:"scoring_mode"`
	Registered     bool             `json:"registered"`
	Problems       []ContestProblem `json:"problems,omitempty"`
	CreatedAt      time.Time        `json:"created_at,omitempty"`
	UpdatedAt      time.Time        `json:"updated_at,omitempty"`
}

type ContestProblem struct {
	ContestID int64  `json:"contest_id"`
	ProblemID int64  `json:"problem_id"`
	Alias     string `json:"alias"`
	SortOrder int32  `json:"sort_order"`
	Title     string `json:"title,omitempty"`
}

type ContestRegistration struct {
	ID           int64     `json:"id"`
	ContestID    int64     `json:"contest_id"`
	UserID       int64     `json:"user_id"`
	DisplayName  string    `json:"display_name"`
	Email        string    `json:"email"`
	Status       string    `json:"status"`
	RegisteredAt time.Time `json:"registered_at"`
}

type ContestProblemResult struct {
	ContestID        int64
	UserID           int64
	ProblemID        int64
	Status           string
	Attempts         int32
	AcceptedAt       *time.Time
	PenaltyMinutes   int32
	LastSubmissionID *int64
	UpdatedAt        time.Time
}

type ContestSubmissionResult struct {
	ID          int64
	ContestID   int64
	UserID      int64
	ProblemID   int64
	Status      string
	SubmittedAt time.Time
	JudgedAt    time.Time
}

type ScoreboardSnapshot struct {
	ID          int64
	ContestID   int64
	View        ScoreboardView
	Board       ScoreboardResponse
	GeneratedAt time.Time
}

type ScoreSnapshotCandidate struct {
	Contest ContestRecord
	View    ScoreboardView
}

type ScoreSnapshotGenerationResult struct {
	Frozen int
	Final  int
}

type ContestInput struct {
	Title       string                `json:"title"`
	Description *string               `json:"description"`
	Visibility  string                `json:"visibility"`
	Status      string                `json:"status"`
	StartAt     time.Time             `json:"start_at"`
	EndAt       time.Time             `json:"end_at"`
	FreezeAt    time.Time             `json:"freeze_at"`
	InviteCode  string                `json:"invite_code"`
	Problems    []ContestProblemInput `json:"problems"`
}

type ContestProblemInput struct {
	ProblemID int64  `json:"problem_id"`
	Alias     string `json:"alias"`
}

type ContestUpdateInput struct {
	Title       *string                `json:"title"`
	Description *string                `json:"description"`
	Visibility  *string                `json:"visibility"`
	Status      *string                `json:"status"`
	StartAt     *time.Time             `json:"start_at"`
	EndAt       *time.Time             `json:"end_at"`
	FreezeAt    *time.Time             `json:"freeze_at"`
	InviteCode  *string                `json:"invite_code"`
	Problems    *[]ContestProblemInput `json:"problems"`
}

type RegistrationInput struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	InviteCode  string `json:"invite_code"`
}

type ListContestFilter struct {
	Status          string
	Visibility      string
	Keyword         string
	VisibleToUserID int64
	IncludePrivate  bool
	Page            int32
	PageSize        int32
	Limit           int32
	Offset          int32
	Cursor          *ContestCursor
}

type ContestCursor struct {
	StartAt time.Time `json:"start_at"`
	ID      int64     `json:"id"`
}

type ContestList struct {
	Items    []ContestRecord `json:"items"`
	Total    int64           `json:"total"`
	Page     int32           `json:"page"`
	PageSize int32           `json:"page_size"`
}

type ContestCursorPage struct {
	Items      []ContestRecord `json:"items"`
	NextCursor *ContestCursor  `json:"next_cursor,omitempty"`
}

type contestTransaction interface {
	CreateContest(context.Context, ContestRecord) (ContestRecord, error)
	UpdateContest(context.Context, int64, ContestUpdateInput) (ContestRecord, error)
	ReplaceContestProblems(context.Context, int64, []ContestProblem) error
}

type Service struct {
	authoring  *ContestAuthoring
	reader     *ContestReader
	policy     *ContestPolicy
	scoreboard *ScoreboardService
	projection *ScoreboardProjection
}

// NewService composes the public contest API from focused workflows.
func NewService(reader *ContestReader, authoring *ContestAuthoring, policy *ContestPolicy, scoreboard *ScoreboardService, projection *ScoreboardProjection) *Service {
	if reader == nil {
		panic("contest reader is required")
	}
	if authoring == nil {
		panic("contest authoring is required")
	}
	if policy == nil {
		panic("contest policy is required")
	}
	if scoreboard == nil {
		panic("contest scoreboard is required")
	}
	if projection == nil {
		panic("contest projection is required")
	}
	return &Service{authoring: authoring, reader: reader, policy: policy, scoreboard: scoreboard, projection: projection}
}

func (s *Service) CreateContest(ctx context.Context, actor auth.Actor, input ContestInput) (ContestRecord, error) {
	return s.authoring.CreateContest(ctx, actor, input)
}

func (s *Service) GetContest(ctx context.Context, actor auth.Actor, id int64) (ContestRecord, error) {
	return s.reader.GetContest(ctx, actor, id)
}

func (s *Service) ListContests(ctx context.Context, actor auth.Actor, filter ListContestFilter) (ContestList, error) {
	return s.reader.ListContests(ctx, actor, filter)
}

func (s *Service) ListContestsByCursor(ctx context.Context, actor auth.Actor, filter ListContestFilter) (ContestCursorPage, error) {
	return s.reader.ListContestsByCursor(ctx, actor, filter)
}

func (s *Service) UpdateContest(ctx context.Context, actor auth.Actor, id int64, input ContestUpdateInput) (ContestRecord, error) {
	return s.authoring.UpdateContest(ctx, actor, id, input)
}

func (s *Service) DeleteContest(ctx context.Context, actor auth.Actor, id int64) (ContestRecord, error) {
	return s.authoring.DeleteContest(ctx, actor, id)
}

func (s *Service) AuthorizeContestRejudge(ctx context.Context, actor auth.Actor, id int64) error {
	return s.policy.AuthorizeContestRejudge(ctx, actor, id)
}

func (s *Service) ValidateContestRejudgeTarget(ctx context.Context, id int64) error {
	return s.policy.ValidateContestRejudgeTarget(ctx, id)
}

func (s *Service) Register(ctx context.Context, actor auth.Actor, contestID int64, input RegistrationInput) (ContestRegistration, error) {
	return s.policy.Register(ctx, actor, contestID, input)
}

func (s *Service) ValidateSubmission(ctx context.Context, actor auth.Actor, problemID, contestID int64) error {
	return s.policy.ValidateSubmission(ctx, actor, problemID, contestID)
}

func (s *Service) SubmissionResultVisibility(ctx context.Context, actor auth.Actor, sub submission.ContestSubmissionVisibility) (submission.SubmissionResultVisibility, error) {
	return s.policy.SubmissionResultVisibility(ctx, actor, sub)
}

func (s *Service) SubmissionResultVisibilities(ctx context.Context, actor auth.Actor, submissions []submission.ContestSubmissionVisibility) (map[int64]submission.SubmissionResultVisibility, error) {
	return s.policy.SubmissionResultVisibilities(ctx, actor, submissions)
}

func (s *Service) Scoreboard(ctx context.Context, actor auth.Actor, contestID int64, requested ScoreboardView) (ScoreboardResponse, error) {
	return s.scoreboard.Scoreboard(ctx, actor, contestID, requested)
}

func (s *Service) GenerateDueScoreSnapshots(ctx context.Context, limit int32) (ScoreSnapshotGenerationResult, error) {
	return s.scoreboard.GenerateDueScoreSnapshots(ctx, limit)
}

func (s *Service) AfterSubmissionTerminal(ctx context.Context, terminal submission.TerminalSubmission) error {
	return s.projection.AfterSubmissionTerminal(ctx, terminal)
}

func requireContestWriter(actor auth.Actor, contest ContestRecord) error {
	if !actor.Authenticated() {
		return apperror.Unauthorized("auth_required", "authentication required")
	}
	if actor.Admin() || actor.UserID == contest.OwnerUserID {
		return nil
	}
	return apperror.Forbidden("contest.not_allowed", "contest access denied")
}

func validateContestInput(input ContestInput) error {
	if strings.TrimSpace(input.Title) == "" {
		return apperror.BadRequest("request.invalid", "title is required")
	}
	if !validVisibility(input.Visibility) {
		return apperror.BadRequest("request.invalid", "visibility is invalid")
	}
	if input.Visibility == VisibilityPrivate && strings.TrimSpace(input.InviteCode) == "" {
		return apperror.BadRequest("contest.invite_code_required", "invite code is required for private contests")
	}
	if !validStatus(input.Status) {
		return apperror.BadRequest("request.invalid", "status is invalid")
	}
	if !input.StartAt.Before(input.EndAt) {
		return apperror.BadRequest("request.invalid", "start_at must be before end_at")
	}
	if input.FreezeAt.Before(input.StartAt) || input.FreezeAt.After(input.EndAt) {
		return apperror.BadRequest("request.invalid", "freeze_at must be within contest window")
	}
	return nil
}

func validateContestUpdate(current ContestRecord, input ContestUpdateInput) error {
	inviteCode := current.InviteCodeHash
	next := ContestInput{
		Title:      current.Title,
		Visibility: current.Visibility,
		Status:     current.Status,
		StartAt:    current.StartAt,
		EndAt:      current.EndAt,
		FreezeAt:   current.FreezeAt,
	}
	if input.Title != nil {
		next.Title = *input.Title
	}
	if input.Visibility != nil {
		next.Visibility = *input.Visibility
	}
	if input.Status != nil {
		next.Status = *input.Status
	}
	if input.StartAt != nil {
		next.StartAt = *input.StartAt
	}
	if input.EndAt != nil {
		next.EndAt = *input.EndAt
	}
	if input.FreezeAt != nil {
		next.FreezeAt = *input.FreezeAt
	}
	if input.InviteCode != nil {
		inviteCode = strings.TrimSpace(*input.InviteCode)
	}
	next.InviteCode = inviteCode
	if next.Visibility == VisibilityPrivate && strings.TrimSpace(inviteCode) == "" {
		return apperror.BadRequest("contest.invite_code_required", "invite code is required for private contests")
	}
	if input.Problems != nil {
		if _, err := contestProblems(current.ID, *input.Problems); err != nil {
			return err
		}
	}
	return validateContestInput(next)
}

func contestProblems(contestID int64, inputs []ContestProblemInput) ([]ContestProblem, error) {
	aliases := make(map[string]struct{}, len(inputs))
	problemIDs := make(map[int64]struct{}, len(inputs))
	out := make([]ContestProblem, 0, len(inputs))
	for i, input := range inputs {
		alias := strings.TrimSpace(input.Alias)
		if input.ProblemID <= 0 || alias == "" {
			return nil, apperror.BadRequest("request.invalid", "contest problem requires problem_id and alias")
		}
		if _, ok := aliases[alias]; ok {
			return nil, apperror.Conflict("contest.problem_alias_conflict", "contest problem alias must be unique")
		}
		if _, ok := problemIDs[input.ProblemID]; ok {
			return nil, apperror.Conflict("contest.problem_conflict", "contest problem must be unique")
		}
		aliases[alias] = struct{}{}
		problemIDs[input.ProblemID] = struct{}{}
		out = append(out, ContestProblem{ContestID: contestID, ProblemID: input.ProblemID, Alias: alias, SortOrder: int32(i + 1)})
	}
	return out, nil
}

func containsProblem(problems []ContestProblem, problemID int64) bool {
	for _, problem := range problems {
		if problem.ProblemID == problemID {
			return true
		}
	}
	return false
}

func hashInviteCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func validVisibility(value string) bool {
	return value == VisibilityPublic || value == VisibilityPrivate
}

func validStatus(value string) bool {
	switch value {
	case StatusDraft, StatusPublished, StatusRunning, StatusEnded, StatusArchived:
		return true
	default:
		return false
	}
}
