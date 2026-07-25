package problem

import (
	"archive/zip"
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
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

	TestcaseStatusReady = "ready"

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
	ID                    int64     `json:"id"`
	OwnerUserID           int64     `json:"owner_user_id"`
	Title                 string    `json:"title"`
	Slug                  string    `json:"slug"`
	Difficulty            string    `json:"difficulty"`
	Visibility            string    `json:"visibility"`
	Status                string    `json:"status"`
	TimeLimitMS           int32     `json:"time_limit_ms"`
	MemoryLimitKB         int32     `json:"memory_limit_kb"`
	CurrentStatementID    int64     `json:"current_statement_id,omitempty"`
	CurrentTestcaseSetID  int64     `json:"current_testcase_set_id,omitempty"`
	CurrentTestcaseStatus string    `json:"current_testcase_status,omitempty"`
	CreatedAt             time.Time `json:"created_at,omitempty"`
	UpdatedAt             time.Time `json:"updated_at,omitempty"`
	PublishedAt           time.Time `json:"published_at,omitempty"`
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
	Status         string    `json:"status"`
	IsCurrent      bool      `json:"is_current"`
	CreatedBy      int64     `json:"created_by"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
}

type ArtifactRecord struct {
	ID             int64
	OwnerType      string
	OwnerID        int64
	Kind           string
	StorageKey     string
	ChecksumSHA256 string
	SizeBytes      int64
	ContentType    string
}

type ProblemStats struct {
	ProblemID           int64            `json:"problem_id"`
	TotalSubmissions    int64            `json:"total_submissions"`
	AcceptedSubmissions int64            `json:"accepted_submissions"`
	StatusCounts        map[string]int64 `json:"status_counts"`
	AcceptanceRate      float64          `json:"acceptance_rate"`
}

type ProblemLimits struct {
	TimeLimitMS   int32 `json:"time_limit_ms"`
	MemoryLimitKB int32 `json:"memory_limit_kb"`
}

type ProblemResponse struct {
	ID          int64         `json:"id"`
	Title       string        `json:"title"`
	Slug        string        `json:"slug"`
	Difficulty  string        `json:"difficulty"`
	Visibility  string        `json:"visibility"`
	Status      string        `json:"status"`
	Tags        []string      `json:"tags"`
	Limits      ProblemLimits `json:"limits"`
	OwnerUserID int64         `json:"owner_user_id"`
	CreatedAt   time.Time     `json:"created_at,omitempty"`
	UpdatedAt   time.Time     `json:"updated_at,omitempty"`
	PublishedAt time.Time     `json:"published_at,omitempty"`
}

type CreateProblemInput struct {
	Title         string   `json:"title"`
	Slug          string   `json:"slug"`
	Difficulty    string   `json:"difficulty"`
	Visibility    string   `json:"visibility"`
	TimeLimitMS   int32    `json:"time_limit_ms"`
	MemoryLimitKB int32    `json:"memory_limit_kb"`
	Tags          []string `json:"tags"`
}

type UpdateProblemInput struct {
	Title         *string  `json:"title"`
	Slug          *string  `json:"slug"`
	Difficulty    *string  `json:"difficulty"`
	Visibility    *string  `json:"visibility"`
	Status        *string  `json:"status"`
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
	Title             string          `json:"title"`
	Description       string          `json:"description"`
	InputDescription  string          `json:"input_description"`
	OutputDescription string          `json:"output_description"`
	Samples           json.RawMessage `json:"samples"`
	Hint              string          `json:"hint"`
	Source            string          `json:"source"`
	MakeCurrent       bool            `json:"make_current"`
}

type AssignTagsInput struct {
	Tags []TagInput `json:"tags"`
}

type TagInput struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type UploadTestcaseInput struct {
	Content        []byte `json:"-"`
	CaseCount      int32  `json:"case_count"`
	ChecksumSHA256 string `json:"checksum_sha256"`
	ContentType    string `json:"content_type"`
}

type ProblemCheckSummary struct {
	FindingCount      int   `json:"finding_count"`
	ErrorCount        int   `json:"error_count"`
	WarningCount      int   `json:"warning_count"`
	InfoCount         int   `json:"info_count"`
	ExpectedCaseCount int32 `json:"expected_case_count"`
	CaseCount         int   `json:"case_count"`
	StorageReadable   bool  `json:"storage_readable"`
	ZipReadable       bool  `json:"zip_readable"`
	Valid             bool  `json:"valid"`
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
}

type ProblemAuthoringState struct {
	Problem     ProblemResponse           `json:"problem"`
	Statement   *Statement                `json:"statement"`
	TestcaseSet *TestcaseSetRecord        `json:"testcase_set"`
	LatestCheck *ProblemCheckRun          `json:"latest_check"`
	Publishable bool                      `json:"publishable"`
	Blockers    []ProblemAuthoringBlocker `json:"blockers"`
}

// Service is the public problem API composed from focused collaborators.
type Service struct {
	reader    *ProblemReader
	authoring *ProblemAuthoring
	checks    *ProblemCheckService
}

// NewService composes the public problem API.
// It panics if a collaborator is nil.
func NewService(reader *ProblemReader, authoring *ProblemAuthoring, checks *ProblemCheckService) *Service {
	if reader == nil {
		panic("problem reader is required")
	}
	if authoring == nil {
		panic("problem authoring is required")
	}
	if checks == nil {
		panic("problem check service is required")
	}
	return &Service{reader: reader, authoring: authoring, checks: checks}
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

func (s *Service) UploadTestcaseArchive(ctx context.Context, actor auth.Actor, problemID int64, input UploadTestcaseInput) (TestcaseSetRecord, error) {
	return s.authoring.UploadTestcaseArchive(ctx, actor, problemID, input)
}

func (s *Service) RunProblemCheck(ctx context.Context, actor auth.Actor, problemID int64) (ProblemCheckResult, error) {
	return s.checks.RunProblemCheck(ctx, actor, problemID)
}

func (s *Service) GetProblemCheck(ctx context.Context, actor auth.Actor, problemID int64, checkID int64) (ProblemCheckResult, error) {
	return s.checks.GetProblemCheck(ctx, actor, problemID, checkID)
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

type problemCheckArchiveValidationResult struct {
	findings    []problemCheckFindingDraft
	caseCount   int
	zipReadable bool
}

func validateProblemCheckArchive(data []byte, set TestcaseSetRecord) problemCheckArchiveValidationResult {
	result := problemCheckArchiveValidationResult{}
	if err := verifyTestcaseArchiveContents(data, defaultTestcaseArchiveLimits); err != nil {
		code := "testcase.zip_invalid"
		message := "testcase archive must be a valid zip file"
		if resourceErr, ok := err.(*testcaseArchiveResourceError); ok {
			code = resourceErr.code
			message = resourceErr.message
		}
		result.findings = append(result.findings, problemCheckFindingDraft{
			severity: ProblemCheckSeverityError,
			code:     code,
			message:  message,
			details:  problemCheckDetails(map[string]any{"storage_key": set.StorageKey}),
		})
		return result
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		result.findings = append(result.findings, problemCheckFindingDraft{
			severity: ProblemCheckSeverityError,
			code:     "testcase.zip_invalid",
			message:  "testcase archive must be a valid zip file",
			details:  problemCheckDetails(map[string]any{"storage_key": set.StorageKey}),
		})
		return result
	}
	result.zipReadable = true

	inputs := map[string]string{}
	outputs := map[string]string{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := path.Base(file.Name)
		lower := strings.ToLower(name)
		matches := caseNameRE.FindStringSubmatch(lower)
		if len(matches) != 2 {
			continue
		}
		if strings.HasPrefix(lower, "input") {
			inputs[matches[1]] = name
		} else {
			outputs[matches[1]] = name
		}
	}

	if len(inputs) == 0 && len(outputs) == 0 {
		result.findings = append(result.findings, problemCheckFindingDraft{
			severity: ProblemCheckSeverityError,
			code:     "testcase.archive_empty",
			message:  "testcase archive has no input/output pairs",
			details:  problemCheckDetails(map[string]any{"storage_key": set.StorageKey}),
		})
	}

	ids := sortedProblemCheckCaseIDs(inputs)
	for _, id := range ids {
		if _, ok := outputs[id]; ok {
			result.caseCount++
			continue
		}
		result.findings = append(result.findings, problemCheckFindingDraft{
			severity:    ProblemCheckSeverityError,
			code:        "testcase.output_missing",
			message:     "each input must have a matching output",
			caseIndex:   problemCheckCaseIndex(id),
			testcaseKey: inputs[id],
			details:     problemCheckDetails(map[string]any{"case_id": id, "input": inputs[id]}),
		})
	}

	ids = sortedProblemCheckCaseIDs(outputs)
	for _, id := range ids {
		if _, ok := inputs[id]; ok {
			continue
		}
		result.findings = append(result.findings, problemCheckFindingDraft{
			severity:    ProblemCheckSeverityError,
			code:        "testcase.input_missing",
			message:     "each output must have a matching input",
			caseIndex:   problemCheckCaseIndex(id),
			testcaseKey: outputs[id],
			details:     problemCheckDetails(map[string]any{"case_id": id, "output": outputs[id]}),
		})
	}

	if int32(result.caseCount) != set.CaseCount {
		result.findings = append(result.findings, problemCheckFindingDraft{
			severity: ProblemCheckSeverityError,
			code:     "testcase.case_count_mismatch",
			message:  "case_count does not match input/output pairs",
			details: problemCheckDetails(map[string]any{
				"expected_case_count": set.CaseCount,
				"actual_case_count":   result.caseCount,
			}),
		})
	}
	return result
}

func validateProblemCheckStatementSamples(statement Statement) []problemCheckFindingDraft {
	samplesJSON := strings.TrimSpace(string(statement.Samples))
	if samplesJSON == "" {
		return nil
	}
	if !strings.HasPrefix(samplesJSON, "[") {
		return []problemCheckFindingDraft{statementSamplesInvalidFinding(0)}
	}

	var samples []map[string]json.RawMessage
	if err := json.Unmarshal(statement.Samples, &samples); err != nil {
		return []problemCheckFindingDraft{statementSamplesInvalidFinding(0)}
	}
	for index, sample := range samples {
		if !problemCheckSampleStringField(sample, "input") || !problemCheckSampleStringField(sample, "output") {
			return []problemCheckFindingDraft{statementSamplesInvalidFinding(index + 1)}
		}
	}
	return nil
}

func statementSamplesInvalidFinding(sampleIndex int) problemCheckFindingDraft {
	details := map[string]any{}
	if sampleIndex > 0 {
		details["sample_index"] = sampleIndex
	}
	return problemCheckFindingDraft{
		severity: ProblemCheckSeverityError,
		code:     "statement.samples_invalid",
		message:  "statement samples must be a JSON array with string input and output fields",
		details:  problemCheckDetails(details),
	}
}

func problemCheckSampleStringField(sample map[string]json.RawMessage, key string) bool {
	raw, ok := sample[key]
	if !ok {
		return false
	}
	var value string
	return json.Unmarshal(raw, &value) == nil
}

func sortedProblemCheckCaseIDs(files map[string]string) []string {
	ids := make([]string, 0, len(files))
	for id := range files {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) < len(ids[j])
		}
		return ids[i] < ids[j]
	})
	return ids
}

func problemCheckCaseIndex(id string) int32 {
	value, err := strconv.ParseInt(id, 10, 32)
	if err != nil {
		return 0
	}
	return int32(value)
}

func problemCheckSummary(expectedCaseCount int32, caseCount int, storageReadable, zipReadable bool, findings []problemCheckFindingDraft) ProblemCheckSummary {
	summary := ProblemCheckSummary{
		FindingCount:      len(findings),
		ExpectedCaseCount: expectedCaseCount,
		CaseCount:         caseCount,
		StorageReadable:   storageReadable,
		ZipReadable:       zipReadable,
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

func requireAuthenticated(actor auth.Actor) error {
	if !actor.Authenticated() {
		return apperror.Unauthorized("auth.required", "authentication required")
	}
	return nil
}

func canWriteProblem(actor auth.Actor, p ProblemRecord) error {
	if err := requireAuthenticated(actor); err != nil {
		return err
	}
	if actor.Admin() || actor.UserID == p.OwnerUserID {
		return nil
	}
	return apperror.Forbidden("problem.forbidden", "problem owner or admin required")
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
	if !validSlug(input.Slug) {
		return apperror.BadRequest("problem.slug_invalid", "slug is invalid")
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
	if input.Slug != nil && !validSlug(*input.Slug) {
		return apperror.BadRequest("problem.slug_invalid", "slug is invalid")
	}
	if input.Difficulty != nil && !validDifficulty(*input.Difficulty) {
		return apperror.BadRequest("problem.difficulty_invalid", "difficulty is invalid")
	}
	if input.Visibility != nil && !validVisibility(*input.Visibility) {
		return apperror.BadRequest("problem.visibility_invalid", "visibility is invalid")
	}
	if input.Status != nil && !validStatus(*input.Status) {
		return apperror.BadRequest("problem.status_invalid", "status is invalid")
	}
	if input.TimeLimitMS != nil && *input.TimeLimitMS <= 0 {
		return apperror.BadRequest("problem.time_limit_invalid", "time_limit_ms must be positive")
	}
	if input.MemoryLimitKB != nil && *input.MemoryLimitKB <= 0 {
		return apperror.BadRequest("problem.memory_limit_invalid", "memory_limit_kb must be positive")
	}
	return nil
}

func validateStatement(input CreateStatementInput) error {
	if strings.TrimSpace(input.Title) == "" {
		return apperror.BadRequest("statement.title_required", "title is required")
	}
	if strings.TrimSpace(input.Description) == "" {
		return apperror.BadRequest("statement.description_required", "description is required")
	}
	if len(input.Samples) == 0 {
		return nil
	}
	if !json.Valid(input.Samples) {
		return apperror.BadRequest("statement.samples_invalid", "samples must be valid JSON")
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

func validStatus(value string) bool {
	switch value {
	case StatusDraft, StatusPublished, StatusArchived:
		return true
	default:
		return false
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func testcaseArchiveKey(problemID int64, checksum string) (string, error) {
	var random [8]byte
	if _, err := crand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate testcase object key: %w", err)
	}
	return fmt.Sprintf("problems/%d/testcases/%s-%s.zip", problemID, checksum, hex.EncodeToString(random[:])), nil
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
		return nil, testcaseArchiveLimitError("testcase.archive_too_large", "testcase archive is too large")
	}
	return data, nil
}
