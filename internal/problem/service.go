package problem

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

const (
	DifficultyEasy   = "easy"
	DifficultyMedium = "medium"
	DifficultyHard   = "hard"

	VisibilityPrivate     = "private"
	VisibilityPublic      = "public"
	VisibilityContestOnly = "contest_only"

	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusArchived  = "archived"

	ProblemCheckStatusQueued    = "queued"
	ProblemCheckStatusRunning   = "running"
	ProblemCheckStatusCompleted = "completed"
	ProblemCheckStatusFailed    = "failed"
	ProblemCheckStatusCanceled  = "canceled"

	ProblemCheckSeverityInfo    = "info"
	ProblemCheckSeverityWarning = "warning"
	ProblemCheckSeverityError   = "error"
)

type ProblemRecord struct {
	ID                   int64     `json:"id"`
	OwnerUserID          int64     `json:"owner_user_id"`
	Title                string    `json:"title"`
	Slug                 string    `json:"slug"`
	Difficulty           string    `json:"difficulty"`
	Visibility           string    `json:"visibility"`
	Status               string    `json:"status"`
	TimeLimitMS          int32     `json:"time_limit_ms"`
	MemoryLimitKB        int32     `json:"memory_limit_kb"`
	CurrentStatementID   int64     `json:"current_statement_id,omitempty"`
	CurrentTestcaseSetID int64     `json:"current_testcase_set_id,omitempty"`
	SubmissionCount      int64     `json:"submission_count"`
	AcceptedCount        int64     `json:"accepted_count"`
	CreatedAt            time.Time `json:"created_at,omitempty"`
	UpdatedAt            time.Time `json:"updated_at,omitempty"`
	PublishedAt          time.Time `json:"published_at,omitempty"`
}

type Statement struct {
	ID                int64           `json:"id"`
	ProblemID         int64           `json:"problem_id"`
	Version           int32           `json:"version"`
	Title             string          `json:"title"`
	Description       string          `json:"description"`
	InputDescription  string          `json:"input_description,omitempty"`
	OutputDescription string          `json:"output_description,omitempty"`
	Samples           json.RawMessage `json:"samples"`
	Hint              string          `json:"hint,omitempty"`
	Source            string          `json:"source,omitempty"`
	IsCurrent         bool            `json:"is_current"`
	CreatedAt         time.Time       `json:"created_at,omitempty"`
}

type Tag struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type TestcaseSetRecord struct {
	ID             int64     `json:"id"`
	ProblemID      int64     `json:"problem_id"`
	Version        int32     `json:"version"`
	StorageKey     string    `json:"storage_key"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	SizeBytes      int64     `json:"size_bytes"`
	CaseCount      int32     `json:"case_count"`
	IsCurrent      bool      `json:"is_current"`
	CreatedBy      int64     `json:"created_by"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
}

// TestcaseSetResponse is the authoring-facing projection of a stored testcase
// set. Storage keys stay server-side; upload responses additionally carry the
// transient validation warnings.
type TestcaseSetResponse struct {
	ID             int64     `json:"id"`
	ProblemID      int64     `json:"problem_id"`
	Version        int32     `json:"version"`
	CaseCount      int32     `json:"case_count"`
	SizeBytes      int64     `json:"size_bytes"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	IsCurrent      bool      `json:"is_current"`
	Warnings       []Finding `json:"warnings,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
}

func testcaseSetResponseFromRecord(record TestcaseSetRecord) TestcaseSetResponse {
	return TestcaseSetResponse{
		ID:             record.ID,
		ProblemID:      record.ProblemID,
		Version:        record.Version,
		CaseCount:      record.CaseCount,
		SizeBytes:      record.SizeBytes,
		ChecksumSHA256: record.ChecksumSHA256,
		IsCurrent:      record.IsCurrent,
		CreatedAt:      record.CreatedAt,
	}
}

type ProblemStats struct {
	ProblemID           int64            `json:"problem_id"`
	TotalSubmissions    int64            `json:"total_submissions"`
	AcceptedSubmissions int64            `json:"accepted_submissions"`
	StatusCounts        map[string]int64 `json:"status_counts"`
	AcceptanceRate      float64          `json:"acceptance_rate"`
}

// ProblemSubmissionCounts is one row of the batched submission projection for a
// page of problems. The reader keys them by problem id, so a list view pays for
// one counts query per page instead of one per problem.
type ProblemSubmissionCounts struct {
	ProblemID       int64
	SubmissionCount int64
	AcceptedCount   int64
}

type ProblemLimits struct {
	TimeLimitMS   int32 `json:"time_limit_ms"`
	MemoryLimitKB int32 `json:"memory_limit_kb"`
}

type ProblemResponse struct {
	ID         int64         `json:"id"`
	Title      string        `json:"title"`
	Slug       string        `json:"slug"`
	Difficulty string        `json:"difficulty"`
	Visibility string        `json:"visibility"`
	Status     string        `json:"status"`
	Tags       []string      `json:"tags"`
	Limits     ProblemLimits `json:"limits"`
	// SubmissionCount and AcceptedCount come from the batched submission
	// projection, not from the problem row itself.
	SubmissionCount int64     `json:"submission_count"`
	AcceptedCount   int64     `json:"accepted_count"`
	OwnerUserID     int64     `json:"owner_user_id"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
	PublishedAt     time.Time `json:"published_at,omitempty"`
}

type CreateProblemInput struct {
	Title         string   `json:"title"`
	Slug          string   `json:"-"`
	Difficulty    string   `json:"difficulty"`
	Visibility    string   `json:"visibility"`
	TimeLimitMS   int32    `json:"time_limit_ms"`
	MemoryLimitKB int32    `json:"memory_limit_kb"`
	Tags          []string `json:"tags"`
}

type UpdateProblemInput struct {
	Title         *string  `json:"title"`
	Difficulty    *string  `json:"difficulty"`
	Visibility    *string  `json:"visibility"`
	Status        *string  `json:"-"`
	TimeLimitMS   *int32   `json:"time_limit_ms"`
	MemoryLimitKB *int32   `json:"memory_limit_kb"`
	Tags          []string `json:"tags"`
}

type ListProblemsFilter struct {
	Difficulty   string
	Status       string
	Visibility   string
	Tag          string
	Keyword      string
	Page         int32
	PageSize     int32
	Limit        int32
	Offset       int32
	ViewerUserID int64
	OwnerUserID  int64
	IncludeAll   bool
	Mine         bool
	Cursor       *ProblemCursor
}

type ProblemCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

type ProblemList struct {
	Items    []ProblemResponse `json:"items"`
	Total    int64             `json:"total"`
	Page     int32             `json:"page"`
	PageSize int32             `json:"page_size"`
}

type ProblemCursorPage struct {
	Items      []ProblemResponse `json:"items"`
	NextCursor *ProblemCursor    `json:"next_cursor,omitempty"`
}

type CreateStatementInput struct {
	Title             string        `json:"-"`
	Description       string        `json:"description"`
	InputDescription  string        `json:"input_description"`
	OutputDescription string        `json:"output_description"`
	Samples           []SampleInput `json:"samples"`
	Hint              string        `json:"hint"`
	Source            string        `json:"source"`
	MakeCurrent       bool          `json:"make_current"`
}

// SampleInput is one typed statement sample. The write boundary rejects empty
// input/output fields and oversized single fields.
type SampleInput struct {
	Input       string `json:"input"`
	Output      string `json:"output"`
	Explanation string `json:"explanation,omitempty"`
}

type AssignTagsInput struct {
	Tags []TagInput `json:"tags"`
}

type TagInput struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type UploadTestcaseInput struct {
	Source      io.ReaderAt
	Size        int64
	ContentType string
}

type ProblemCheckSummary struct {
	FindingCount    int  `json:"finding_count"`
	ErrorCount      int  `json:"error_count"`
	WarningCount    int  `json:"warning_count"`
	InfoCount       int  `json:"info_count"`
	CaseCount       int  `json:"case_count"`
	StorageReadable bool `json:"storage_readable"`
	ZipReadable     bool `json:"zip_readable"`
	Valid           bool `json:"valid"`
}

type ProblemCheckRun struct {
	ID            int64                 `json:"id"`
	ProblemID     int64                 `json:"problem_id"`
	StatementID   int64                 `json:"statement_id,omitempty"`
	TestcaseSetID int64                 `json:"testcase_set_id,omitempty"`
	RequestedBy   int64                 `json:"requested_by,omitempty"`
	Status        string                `json:"status"`
	Summary       ProblemCheckSummary   `json:"summary"`
	ErrorMessage  string                `json:"error_message,omitempty"`
	Findings      []ProblemCheckFinding `json:"findings"`
	StartedAt     time.Time             `json:"started_at,omitempty"`
	FinishedAt    time.Time             `json:"finished_at,omitempty"`
	CreatedAt     time.Time             `json:"created_at,omitempty"`
	UpdatedAt     time.Time             `json:"updated_at,omitempty"`
}

type ProblemCheckFinding struct {
	ID          int64           `json:"id"`
	RunID       int64           `json:"run_id"`
	Severity    string          `json:"severity"`
	Code        string          `json:"code"`
	Message     string          `json:"message"`
	CaseIndex   int32           `json:"case_index,omitempty"`
	TestcaseKey string          `json:"testcase_key,omitempty"`
	Details     json.RawMessage `json:"details"`
	CreatedAt   time.Time       `json:"created_at,omitempty"`
}

type ProblemCheckResult struct {
	Run      ProblemCheckRun       `json:"run"`
	Findings []ProblemCheckFinding `json:"findings"`
}

type ProblemAuthoringBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Step    string `json:"step"`
}

type ProblemAuthoringState struct {
	Problem     ProblemResponse           `json:"problem"`
	Statement   *Statement                `json:"statement"`
	TestcaseSet *TestcaseSetResponse      `json:"testcase_set"`
	LatestCheck *ProblemCheckRun          `json:"latest_check"`
	Flow        ProblemAuthoringFlow      `json:"flow"`
	Publishable bool                      `json:"publishable"`
	Blockers    []ProblemAuthoringBlocker `json:"blockers"`
}

// Service is the public problem API composed from focused collaborators.
type Service struct {
	reader    *ProblemReader
	authoring *ProblemAuthoring
	checks    *ProblemCheckService
	review    *ProblemReviewService
}

// NewService composes the public problem API.
// It panics if a collaborator is nil.
func NewService(reader *ProblemReader, authoring *ProblemAuthoring, checks *ProblemCheckService, review ...*ProblemReviewService) *Service {
	if reader == nil {
		panic("problem reader is required")
	}
	if authoring == nil {
		panic("problem authoring is required")
	}
	if checks == nil {
		panic("problem check service is required")
	}
	var reviewService *ProblemReviewService
	if len(review) > 0 {
		reviewService = review[0]
	}
	return &Service{reader: reader, authoring: authoring, checks: checks, review: reviewService}
}

func (s *Service) CreateProblem(ctx context.Context, actor auth.Actor, input CreateProblemInput) (ProblemRecord, error) {
	return s.authoring.CreateProblem(ctx, actor, input)
}

func (s *Service) GetProblem(ctx context.Context, actor auth.Actor, id int64) (ProblemRecord, error) {
	return s.reader.GetProblem(ctx, actor, id)
}

func (s *Service) ListProblems(ctx context.Context, actor auth.Actor, filter ListProblemsFilter) (ProblemList, error) {
	return s.reader.ListProblems(ctx, actor, filter)
}

func (s *Service) ListProblemsByCursor(ctx context.Context, actor auth.Actor, filter ListProblemsFilter) (ProblemCursorPage, error) {
	return s.reader.ListProblemsByCursor(ctx, actor, filter)
}

func (s *Service) GetProblemAuthoringState(ctx context.Context, actor auth.Actor, id int64) (ProblemAuthoringState, error) {
	return s.reader.GetProblemAuthoringState(ctx, actor, id)
}

func (s *Service) UpdateProblem(ctx context.Context, actor auth.Actor, id int64, input UpdateProblemInput) (ProblemRecord, error) {
	return s.authoring.UpdateProblem(ctx, actor, id, input)
}

func (s *Service) ArchiveProblem(ctx context.Context, actor auth.Actor, id int64) (ProblemRecord, error) {
	return s.authoring.ArchiveProblem(ctx, actor, id)
}

func (s *Service) CreateStatement(ctx context.Context, actor auth.Actor, problemID int64, input CreateStatementInput) (Statement, error) {
	return s.authoring.CreateStatement(ctx, actor, problemID, input)
}

func (s *Service) CurrentStatement(ctx context.Context, actor auth.Actor, problemID int64) (Statement, error) {
	return s.reader.CurrentStatement(ctx, actor, problemID)
}

func (s *Service) AssignTags(ctx context.Context, actor auth.Actor, problemID int64, input AssignTagsInput) ([]Tag, error) {
	return s.authoring.AssignTags(ctx, actor, problemID, input)
}

func (s *Service) UploadTestcaseArchive(ctx context.Context, actor auth.Actor, problemID int64, input UploadTestcaseInput) (TestcaseSetRecord, []Finding, error) {
	return s.authoring.UploadTestcaseArchive(ctx, actor, problemID, input)
}

func (s *Service) RunProblemCheck(ctx context.Context, actor auth.Actor, problemID int64) (ProblemCheckResult, error) {
	return s.checks.RunProblemCheck(ctx, actor, problemID)
}

func (s *Service) GetProblemCheck(ctx context.Context, actor auth.Actor, problemID int64, checkID int64) (ProblemCheckResult, error) {
	return s.checks.GetProblemCheck(ctx, actor, problemID, checkID)
}

func (s *Service) SubmitProblemReview(ctx context.Context, actor auth.Actor, problemID int64) (ProblemResponse, error) {
	if s.review == nil {
		return ProblemResponse{}, apperror.ServiceUnavailable("problem review service unavailable")
	}
	problem, err := s.review.Submit(ctx, actor, problemID)
	if err != nil {
		return ProblemResponse{}, err
	}
	return s.reader.ProblemResponse(ctx, problem)
}

func (s *Service) ListProblemReviewQueue(ctx context.Context, actor auth.Actor, filter ProblemReviewQueueFilter) (ProblemReviewQueue, error) {
	if s.review == nil {
		return ProblemReviewQueue{}, apperror.ServiceUnavailable("problem review service unavailable")
	}
	return s.review.Queue(ctx, actor, filter)
}

func (s *Service) DecideProblemReview(ctx context.Context, actor auth.Actor, problemID int64, input ProblemReviewDecisionInput) (ProblemResponse, error) {
	if s.review == nil {
		return ProblemResponse{}, apperror.ServiceUnavailable("problem review service unavailable")
	}
	problem, err := s.review.Decide(ctx, actor, problemID, input)
	if err != nil {
		return ProblemResponse{}, err
	}
	return s.reader.ProblemResponse(ctx, problem)
}

func (s *Service) ListProblemReviewEvents(ctx context.Context, actor auth.Actor, problemID int64) ([]ProblemReviewEvent, error) {
	if s.review == nil {
		return nil, apperror.ServiceUnavailable("problem review service unavailable")
	}
	return s.review.Events(ctx, actor, problemID)
}

func (s *Service) ProblemResponse(ctx context.Context, p ProblemRecord) (ProblemResponse, error) {
	return s.reader.ProblemResponse(ctx, p)
}

func (s *Service) CurrentReadyTestcaseSet(ctx context.Context, problemID int64) (TestcaseSet, error) {
	return s.reader.CurrentReadyTestcaseSet(ctx, problemID)
}

func (s *Service) GetForJudge(ctx context.Context, problemID int64) (Problem, error) {
	return s.reader.GetForJudge(ctx, problemID)
}

func (s *Service) AuthorizeProblemRejudge(ctx context.Context, actor auth.Actor, id int64) error {
	return s.reader.AuthorizeProblemRejudge(ctx, actor, id)
}

func (s *Service) Stats(ctx context.Context, actor auth.Actor, problemID int64) (ProblemStats, error) {
	return s.reader.Stats(ctx, actor, problemID)
}

type problemCheckFindingDraft struct {
	severity    string
	code        string
	message     string
	caseIndex   int32
	testcaseKey string
	details     json.RawMessage
}

func problemCheckSummary(caseCount int, storageReadable, zipReadable bool, findings []problemCheckFindingDraft) ProblemCheckSummary {
	summary := ProblemCheckSummary{
		FindingCount:    len(findings),
		CaseCount:       caseCount,
		StorageReadable: storageReadable,
		ZipReadable:     zipReadable,
	}
	for _, finding := range findings {
		switch finding.severity {
		case ProblemCheckSeverityError:
			summary.ErrorCount++
		case ProblemCheckSeverityWarning:
			summary.WarningCount++
		case ProblemCheckSeverityInfo:
			summary.InfoCount++
		}
	}
	summary.Valid = summary.ErrorCount == 0
	return summary
}

func marshalProblemCheckSummary(summary ProblemCheckSummary) (json.RawMessage, error) {
	data, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func problemCheckDetails(value map[string]any) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(data)
}

func problemCheckNotFoundErr(err error) error {
	if appErr, ok := apperror.From(err); ok && appErr.HTTPStatus == http.StatusNotFound {
		return apperror.NotFound("problem_check.not_found", "problem check not found")
	}
	return err
}

func canWriteProblem(actor auth.Actor, p ProblemRecord) error {
	return (RBACProblemPolicy{}).CanEdit(actor, p)
}

func canReadProblem(actor auth.Actor, p ProblemRecord) error {
	if p.Status == StatusPublished && p.Visibility == VisibilityPublic {
		return nil
	}
	if actor.Admin() || (actor.Authenticated() && actor.UserID == p.OwnerUserID) {
		return nil
	}
	return apperror.NotFound("problem.not_found", "problem not found")
}

func validateCreateProblem(input CreateProblemInput) error {
	if strings.TrimSpace(input.Title) == "" {
		return apperror.BadRequest("problem.title_required", "title is required")
	}
	if !validDifficulty(input.Difficulty) {
		return apperror.BadRequest("problem.difficulty_invalid", "difficulty is invalid")
	}
	if !validVisibility(input.Visibility) {
		return apperror.BadRequest("problem.visibility_invalid", "visibility is invalid")
	}
	if input.TimeLimitMS <= 0 {
		return apperror.BadRequest("problem.time_limit_invalid", "time_limit_ms must be positive")
	}
	if input.MemoryLimitKB <= 0 {
		return apperror.BadRequest("problem.memory_limit_invalid", "memory_limit_kb must be positive")
	}
	return nil
}

func validateUpdateProblem(input UpdateProblemInput) error {
	if input.Difficulty != nil && !validDifficulty(*input.Difficulty) {
		return apperror.BadRequest("problem.difficulty_invalid", "difficulty is invalid")
	}
	if input.Visibility != nil && !validVisibility(*input.Visibility) {
		return apperror.BadRequest("problem.visibility_invalid", "visibility is invalid")
	}
	if input.TimeLimitMS != nil && *input.TimeLimitMS <= 0 {
		return apperror.BadRequest("problem.time_limit_invalid", "time_limit_ms must be positive")
	}
	if input.MemoryLimitKB != nil && *input.MemoryLimitKB <= 0 {
		return apperror.BadRequest("problem.memory_limit_invalid", "memory_limit_kb must be positive")
	}
	return nil
}

const maxSampleFieldBytes = 64 << 10

func validateStatement(input CreateStatementInput) error {
	if strings.TrimSpace(input.Description) == "" {
		return apperror.BadRequest("statement.description_required", "description is required")
	}
	for _, sample := range input.Samples {
		if strings.TrimSpace(sample.Input) == "" || strings.TrimSpace(sample.Output) == "" {
			return apperror.BadRequest("statement.sample_invalid", "sample input and output are required")
		}
		for _, field := range []string{sample.Input, sample.Output, sample.Explanation} {
			if len(field) > maxSampleFieldBytes {
				return apperror.BadRequest("statement.sample_too_large", "sample field must not exceed 64 KiB")
			}
		}
	}
	return nil
}

func validateTags(tags []TagInput) error {
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		if strings.TrimSpace(tag.Name) == "" {
			return apperror.BadRequest("tag.name_required", "tag name is required")
		}
		if !validSlug(tag.Slug) {
			return apperror.BadRequest("tag.slug_invalid", "tag slug is invalid")
		}
		if _, ok := seen[tag.Slug]; ok {
			return apperror.BadRequest("tag.duplicate", "duplicate tag slug")
		}
		seen[tag.Slug] = struct{}{}
	}
	return nil
}

func tagInputsFromNames(names []string) ([]TagInput, error) {
	tags := make([]TagInput, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return nil, apperror.BadRequest("tag.name_required", "tag name is required")
		}
		tags = append(tags, TagInput{Name: trimmed, Slug: slugifyTag(trimmed)})
	}
	if err := validateTags(tags); err != nil {
		return nil, err
	}
	return tags, nil
}

func slugifyTag(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var builder strings.Builder
	lastDash := false
	for _, r := range lower {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func normalizeListFilter(actor auth.Actor, filter ListProblemsFilter) ListProblemsFilter {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	filter.Limit = filter.PageSize
	filter.Offset = (filter.Page - 1) * filter.PageSize
	if filter.Mine && actor.Authenticated() {
		filter.OwnerUserID = actor.UserID
		filter.ViewerUserID = actor.UserID
		filter.IncludeAll = false
		return filter
	}
	if actor.Admin() {
		filter.IncludeAll = true
		return filter
	}
	if actor.Authenticated() {
		filter.ViewerUserID = actor.UserID
	}
	return filter
}

var slugRE = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)

func validSlug(slug string) bool {
	return slugRE.MatchString(strings.TrimSpace(slug))
}

func validDifficulty(value string) bool {
	switch value {
	case DifficultyEasy, DifficultyMedium, DifficultyHard:
		return true
	default:
		return false
	}
}

func validVisibility(value string) bool {
	switch value {
	case VisibilityPrivate, VisibilityPublic, VisibilityContestOnly:
		return true
	default:
		return false
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func testcaseArchiveKey(problemID int64) (string, error) {
	var random [8]byte
	if _, err := crand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate testcase object key: %w", err)
	}
	return fmt.Sprintf("problems/%d/testcases/%s.zip", problemID, hex.EncodeToString(random[:])), nil
}

func readAllAndClose(body io.ReadCloser, maxBytes int64) ([]byte, error) {
	defer func() { _ = body.Close() }()
	reader := io.Reader(body)
	if maxBytes > 0 {
		reader = io.LimitReader(body, maxBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, apperror.New(codeArchiveTooLarge, "testcase archive is too large", http.StatusRequestEntityTooLarge)
	}
	return data, nil
}
