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
	Problems       []ContestProblem `json:"problems,omitempty"`
	CreatedAt      time.Time        `json:"created_at,omitempty"`
	UpdatedAt      time.Time        `json:"updated_at,omitempty"`
}

type ContestProblem struct {
	ContestID int64  `json:"contest_id"`
	ProblemID int64  `json:"problem_id"`
	Alias     string `json:"alias"`
	SortOrder int32  `json:"sort_order"`
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
}

type ContestList struct {
	Items    []ContestRecord `json:"items"`
	Total    int64           `json:"total"`
	Page     int32           `json:"page"`
	PageSize int32           `json:"page_size"`
}

type Service struct {
	repo Repository
	now  func() time.Time
}

type Option func(*Service)

func WithNow(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

func NewService(repo Repository, options ...Option) *Service {
	s := &Service{repo: repo, now: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		option(s)
	}
	return s
}

func (s *Service) CreateContest(ctx context.Context, actor auth.Actor, input ContestInput) (ContestRecord, error) {
	if !actor.Authenticated() {
		return ContestRecord{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	if input.Status == "" {
		input.Status = StatusDraft
	}
	if err := validateContestInput(input); err != nil {
		return ContestRecord{}, err
	}
	problems, err := contestProblems(0, input.Problems)
	if err != nil {
		return ContestRecord{}, err
	}
	record := ContestRecord{
		OwnerUserID:    actor.UserID,
		Title:          strings.TrimSpace(input.Title),
		Description:    input.Description,
		Visibility:     input.Visibility,
		Status:         input.Status,
		StartAt:        input.StartAt.UTC(),
		EndAt:          input.EndAt.UTC(),
		FreezeAt:       input.FreezeAt.UTC(),
		InviteCodeHash: hashInviteCode(input.InviteCode),
	}

	var created ContestRecord
	err = s.repo.WithTx(ctx, func(ctx context.Context, repo Repository) error {
		var err error
		created, err = repo.CreateContest(ctx, record)
		if err != nil {
			return err
		}
		for i := range problems {
			problems[i].ContestID = created.ID
		}
		if err := repo.ReplaceContestProblems(ctx, created.ID, problems); err != nil {
			return err
		}
		created.Problems = problems
		return nil
	})
	return created, err
}

func (s *Service) GetContest(ctx context.Context, actor auth.Actor, id int64) (ContestRecord, error) {
	record, err := s.repo.GetContest(ctx, id)
	if err != nil {
		return ContestRecord{}, err
	}
	if err := s.canReadContest(ctx, actor, record); err != nil {
		return ContestRecord{}, err
	}
	return s.withProblems(ctx, record)
}

func (s *Service) ListContests(ctx context.Context, actor auth.Actor, filter ListContestFilter) (ContestList, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	if actor.Admin() {
		filter.IncludePrivate = true
	} else if actor.Authenticated() {
		filter.VisibleToUserID = actor.UserID
	}
	filter.Limit = filter.PageSize
	filter.Offset = (filter.Page - 1) * filter.PageSize
	items, total, err := s.repo.ListContests(ctx, filter)
	if err != nil {
		return ContestList{}, err
	}
	for i := range items {
		withProblems, err := s.withProblems(ctx, items[i])
		if err != nil {
			return ContestList{}, err
		}
		items[i] = withProblems
	}
	return ContestList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (s *Service) UpdateContest(ctx context.Context, actor auth.Actor, id int64, input ContestUpdateInput) (ContestRecord, error) {
	current, err := s.repo.GetContest(ctx, id)
	if err != nil {
		return ContestRecord{}, err
	}
	if err := requireContestWriter(actor, current); err != nil {
		return ContestRecord{}, err
	}
	if err := validateContestUpdate(current, input); err != nil {
		return ContestRecord{}, err
	}
	var updated ContestRecord
	err = s.repo.WithTx(ctx, func(ctx context.Context, repo Repository) error {
		var err error
		updated, err = repo.UpdateContest(ctx, id, input)
		if err != nil {
			return err
		}
		if input.Problems != nil {
			problems, err := contestProblems(id, *input.Problems)
			if err != nil {
				return err
			}
			if err := repo.ReplaceContestProblems(ctx, id, problems); err != nil {
				return err
			}
			updated.Problems = problems
			return nil
		}
		updated, err = s.withProblems(ctx, updated)
		return err
	})
	return updated, err
}

func (s *Service) DeleteContest(ctx context.Context, actor auth.Actor, id int64) (ContestRecord, error) {
	current, err := s.repo.GetContest(ctx, id)
	if err != nil {
		return ContestRecord{}, err
	}
	if err := requireContestWriter(actor, current); err != nil {
		return ContestRecord{}, err
	}
	return s.repo.ArchiveContest(ctx, id)
}

func (s *Service) Register(ctx context.Context, actor auth.Actor, contestID int64, input RegistrationInput) (ContestRegistration, error) {
	if !actor.Authenticated() {
		return ContestRegistration{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	contest, err := s.repo.GetContest(ctx, contestID)
	if err != nil {
		return ContestRegistration{}, err
	}
	if contest.Visibility == VisibilityPrivate && contest.InviteCodeHash == "" {
		return ContestRegistration{}, apperror.Forbidden("contest.invite_code_required", "invite code is required")
	}
	if contest.Visibility == VisibilityPrivate && contest.InviteCodeHash != hashInviteCode(input.InviteCode) {
		return ContestRegistration{}, apperror.Forbidden("contest.invite_code_invalid", "invite code is invalid")
	}
	displayName := strings.TrimSpace(input.DisplayName)
	email := strings.TrimSpace(input.Email)
	if displayName == "" || email == "" {
		return ContestRegistration{}, apperror.BadRequest("request.invalid", "display_name and email are required")
	}
	return s.repo.CreateRegistration(ctx, ContestRegistration{
		ContestID:   contestID,
		UserID:      actor.UserID,
		DisplayName: displayName,
		Email:       email,
		Status:      RegistrationActive,
	})
}

func (s *Service) ValidateSubmission(ctx context.Context, actor auth.Actor, problemID, contestID int64) error {
	if !actor.Authenticated() {
		return apperror.Unauthorized("auth_required", "authentication required")
	}
	contest, err := s.repo.GetContest(ctx, contestID)
	if err != nil {
		return err
	}
	if contest.Status != StatusPublished && contest.Status != StatusRunning {
		return apperror.Forbidden("contest.not_started", "contest is not accepting submissions")
	}
	now := s.now()
	if now.Before(contest.StartAt) {
		return apperror.Forbidden("contest.not_started", "contest has not started")
	}
	if !now.Before(contest.EndAt) {
		return apperror.Forbidden("contest.ended", "contest has ended")
	}
	problems, err := s.repo.ListContestProblems(ctx, contestID)
	if err != nil {
		return err
	}
	if !containsProblem(problems, problemID) {
		return apperror.NotFound("contest.problem_not_found", "problem is not in contest")
	}
	if actor.Admin() || actor.UserID == contest.OwnerUserID {
		return nil
	}
	registration, err := s.repo.GetRegistration(ctx, contestID, actor.UserID)
	if err != nil || registration.Status != RegistrationActive {
		return apperror.Forbidden("contest.registration_required", "contest registration required")
	}
	return nil
}

func (s *Service) AfterSubmissionTerminal(ctx context.Context, terminal submission.TerminalSubmission) error {
	if terminal.ContestID == nil {
		return nil
	}
	return s.recordTerminalSubmission(ctx, terminal)
}

func (s *Service) withProblems(ctx context.Context, record ContestRecord) (ContestRecord, error) {
	problems, err := s.repo.ListContestProblems(ctx, record.ID)
	if err != nil {
		return ContestRecord{}, err
	}
	record.Problems = problems
	return record, nil
}

func (s *Service) canReadContest(ctx context.Context, actor auth.Actor, contest ContestRecord) error {
	if contest.Visibility == VisibilityPublic || actor.Admin() || actor.UserID == contest.OwnerUserID {
		return nil
	}
	if !actor.Authenticated() {
		return apperror.Unauthorized("auth_required", "authentication required")
	}
	registration, err := s.repo.GetRegistration(ctx, contest.ID, actor.UserID)
	if err == nil && registration.Status == RegistrationActive {
		return nil
	}
	return apperror.Forbidden("contest.not_allowed", "contest access denied")
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
