package problem

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/storage"
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
	IncludeAll   bool
}

type ProblemList struct {
	Items    []ProblemResponse `json:"items"`
	Total    int64             `json:"total"`
	Page     int32             `json:"page"`
	PageSize int32             `json:"page_size"`
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

type Service struct {
	repo    Repository
	storage storage.ObjectStorage
	now     func() time.Time
}

func NewService(repo Repository, objectStorage storage.ObjectStorage) *Service {
	return &Service{repo: repo, storage: objectStorage, now: time.Now}
}

func (s *Service) CreateProblem(ctx context.Context, actor auth.Actor, input CreateProblemInput) (ProblemRecord, error) {
	if err := requireAuthenticated(actor); err != nil {
		return ProblemRecord{}, err
	}
	if err := validateCreateProblem(input); err != nil {
		return ProblemRecord{}, err
	}
	tagInputs, err := tagInputsFromNames(input.Tags)
	if err != nil {
		return ProblemRecord{}, err
	}
	var created ProblemRecord
	err = s.repo.WithTx(ctx, func(ctx context.Context, repo Repository) error {
		var err error
		created, err = repo.CreateProblem(ctx, actor.UserID, input)
		if err != nil {
			return err
		}
		if len(tagInputs) > 0 {
			_, err = repo.ReplaceProblemTags(ctx, created.ID, tagInputs)
		}
		return err
	})
	return created, err
}

func (s *Service) GetProblem(ctx context.Context, actor auth.Actor, id int64) (ProblemRecord, error) {
	p, err := s.repo.GetProblem(ctx, id)
	if err != nil {
		return ProblemRecord{}, err
	}
	if err := canReadProblem(actor, p); err != nil {
		return ProblemRecord{}, err
	}
	return p, nil
}

func (s *Service) ListProblems(ctx context.Context, actor auth.Actor, filter ListProblemsFilter) (ProblemList, error) {
	filter = normalizeListFilter(actor, filter)
	items, err := s.repo.ListProblems(ctx, filter)
	if err != nil {
		return ProblemList{}, err
	}
	total, err := s.repo.CountProblems(ctx, filter)
	if err != nil {
		return ProblemList{}, err
	}
	responses := make([]ProblemResponse, 0, len(items))
	for _, item := range items {
		response, err := s.ProblemResponse(ctx, item)
		if err != nil {
			return ProblemList{}, err
		}
		responses = append(responses, response)
	}
	return ProblemList{Items: responses, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (s *Service) UpdateProblem(ctx context.Context, actor auth.Actor, id int64, input UpdateProblemInput) (ProblemRecord, error) {
	var updated ProblemRecord
	err := s.repo.WithTx(ctx, func(ctx context.Context, repo Repository) error {
		current, err := repo.LockProblemForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if err := canWriteProblem(actor, current); err != nil {
			return err
		}
		if input.Status != nil && *input.Status == StatusPublished {
			if err := ensurePublishable(ctx, repo, id); err != nil {
				return err
			}
		}
		if err := validateUpdateProblem(input); err != nil {
			return err
		}
		tagInputs, err := tagInputsFromNames(input.Tags)
		if err != nil {
			return err
		}
		updated, err = repo.UpdateProblem(ctx, id, input)
		if err != nil {
			return err
		}
		if input.Tags != nil {
			_, err = repo.ReplaceProblemTags(ctx, id, tagInputs)
		}
		return err
	})
	return updated, err
}

func (s *Service) ArchiveProblem(ctx context.Context, actor auth.Actor, id int64) (ProblemRecord, error) {
	var archived ProblemRecord
	err := s.repo.WithTx(ctx, func(ctx context.Context, repo Repository) error {
		current, err := repo.LockProblemForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if err := canWriteProblem(actor, current); err != nil {
			return err
		}
		archived, err = repo.ArchiveProblem(ctx, id)
		return err
	})
	return archived, err
}

func (s *Service) CreateStatement(ctx context.Context, actor auth.Actor, problemID int64, input CreateStatementInput) (Statement, error) {
	if input.MakeCurrent == false {
		input.MakeCurrent = true
	}
	if err := validateStatement(input); err != nil {
		return Statement{}, err
	}
	var statement Statement
	err := s.repo.WithTx(ctx, func(ctx context.Context, repo Repository) error {
		p, err := repo.LockProblemForUpdate(ctx, problemID)
		if err != nil {
			return err
		}
		if err := canWriteProblem(actor, p); err != nil {
			return err
		}
		version, err := repo.NextProblemStatementVersion(ctx, problemID)
		if err != nil {
			return err
		}
		if input.MakeCurrent {
			if err := repo.ClearCurrentProblemStatement(ctx, problemID); err != nil {
				return err
			}
		}
		statement, err = repo.CreateProblemStatement(ctx, problemID, version, input)
		return err
	})
	return statement, err
}

func (s *Service) CurrentStatement(ctx context.Context, actor auth.Actor, problemID int64) (Statement, error) {
	p, err := s.repo.GetProblem(ctx, problemID)
	if err != nil {
		return Statement{}, err
	}
	if err := canReadProblem(actor, p); err != nil {
		return Statement{}, err
	}
	return s.repo.GetCurrentProblemStatement(ctx, problemID)
}

func (s *Service) AssignTags(ctx context.Context, actor auth.Actor, problemID int64, input AssignTagsInput) ([]Tag, error) {
	if err := validateTags(input.Tags); err != nil {
		return nil, err
	}
	var tags []Tag
	err := s.repo.WithTx(ctx, func(ctx context.Context, repo Repository) error {
		p, err := repo.LockProblemForUpdate(ctx, problemID)
		if err != nil {
			return err
		}
		if err := canWriteProblem(actor, p); err != nil {
			return err
		}
		tags, err = repo.ReplaceProblemTags(ctx, problemID, input.Tags)
		return err
	})
	return tags, err
}

func (s *Service) UploadTestcaseArchive(ctx context.Context, actor auth.Actor, problemID int64, input UploadTestcaseInput) (TestcaseSetRecord, error) {
	if s.storage == nil {
		return TestcaseSetRecord{}, apperror.ServiceUnavailable("object storage unavailable")
	}
	if err := validateTestcaseArchive(input.Content, input.CaseCount, input.ChecksumSHA256, defaultMaxTestcaseArchiveBytes); err != nil {
		return TestcaseSetRecord{}, err
	}
	current, err := s.repo.GetProblem(ctx, problemID)
	if err != nil {
		return TestcaseSetRecord{}, err
	}
	if err := canWriteProblem(actor, current); err != nil {
		return TestcaseSetRecord{}, err
	}

	actualChecksum := sha256Hex(input.Content)
	contentType := input.ContentType
	if contentType == "" {
		contentType = "application/zip"
	}
	key, err := testcaseArchiveKey(problemID, actualChecksum)
	if err != nil {
		return TestcaseSetRecord{}, err
	}
	if _, err := s.storage.Put(ctx, storage.Object{
		Key:         key,
		ContentType: contentType,
		Size:        int64(len(input.Content)),
		Metadata: map[string]string{
			"problem-id": fmt.Sprint(problemID),
			"sha256":     actualChecksum,
		},
		Body: bytes.NewReader(input.Content),
	}); err != nil {
		return TestcaseSetRecord{}, err
	}

	var created TestcaseSetRecord
	err = s.repo.WithTx(ctx, func(ctx context.Context, repo Repository) error {
		p, err := repo.LockProblemForUpdate(ctx, problemID)
		if err != nil {
			return err
		}
		if err := canWriteProblem(actor, p); err != nil {
			return err
		}
		version, err := repo.NextTestcaseSetVersion(ctx, problemID)
		if err != nil {
			return err
		}
		artifact, err := repo.CreateArtifact(ctx, ArtifactRecord{
			OwnerType:      "testcase",
			OwnerID:        problemID,
			Kind:           "testcase_archive",
			StorageKey:     key,
			ChecksumSHA256: actualChecksum,
			SizeBytes:      int64(len(input.Content)),
			ContentType:    contentType,
		})
		if err != nil {
			return err
		}
		if artifact.ID == 0 {
			return apperror.Internal()
		}
		if err := repo.ClearCurrentTestcaseSet(ctx, problemID); err != nil {
			return err
		}
		created, err = repo.CreateTestcaseSet(ctx, problemID, version, key, actualChecksum, int64(len(input.Content)), input.CaseCount, actor.UserID)
		return err
	})
	if err != nil {
		_ = s.storage.Delete(ctx, key)
	}
	return created, err
}

func (s *Service) ProblemResponse(ctx context.Context, p ProblemRecord) (ProblemResponse, error) {
	tags, err := s.repo.ListProblemTags(ctx, p.ID)
	if err != nil {
		return ProblemResponse{}, err
	}
	tagNames := make([]string, 0, len(tags))
	for _, tag := range tags {
		tagNames = append(tagNames, tag.Name)
	}
	return ProblemResponse{
		ID:         p.ID,
		Title:      p.Title,
		Slug:       p.Slug,
		Difficulty: p.Difficulty,
		Visibility: p.Visibility,
		Status:     p.Status,
		Tags:       tagNames,
		Limits: ProblemLimits{
			TimeLimitMS:   p.TimeLimitMS,
			MemoryLimitKB: p.MemoryLimitKB,
		},
		OwnerUserID: p.OwnerUserID,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
		PublishedAt: p.PublishedAt,
	}, nil
}

func (s *Service) CurrentReadyTestcaseSet(ctx context.Context, problemID int64) (TestcaseSet, error) {
	set, err := s.repo.GetCurrentReadyTestcaseSet(ctx, problemID)
	if err != nil {
		return TestcaseSet{}, err
	}
	if s.storage == nil {
		return TestcaseSet{}, apperror.ServiceUnavailable("testcase object storage unavailable")
	}
	if strings.TrimSpace(set.StorageKey) == "" {
		return TestcaseSet{}, apperror.BadRequest("testcase.archive_missing", "testcase archive storage key is missing")
	}
	p, err := s.repo.GetProblem(ctx, problemID)
	if err != nil {
		return TestcaseSet{}, err
	}
	body, _, err := s.storage.Get(ctx, set.StorageKey)
	if err != nil {
		return TestcaseSet{}, err
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return TestcaseSet{}, err
	}
	cases, err := parseTestcaseArchiveCases(data, time.Duration(p.TimeLimitMS)*time.Millisecond, int64(p.MemoryLimitKB))
	if err != nil {
		return TestcaseSet{}, err
	}
	if set.CaseCount > 0 && int32(len(cases)) != set.CaseCount {
		return TestcaseSet{}, apperror.BadRequest("testcase.case_count_mismatch", "case_count does not match input/output pairs")
	}
	return TestcaseSet{
		ID:        set.ID,
		ProblemID: set.ProblemID,
		Version:   int(set.Version),
		Status:    set.Status,
		Cases:     cases,
	}, nil
}

func (s *Service) GetForJudge(ctx context.Context, problemID int64) (Problem, error) {
	p, err := s.repo.GetProblem(ctx, problemID)
	if err != nil {
		return Problem{}, err
	}
	if p.Status != StatusPublished || p.CurrentStatementID == 0 || p.CurrentTestcaseSetID == 0 || p.CurrentTestcaseStatus != TestcaseStatusReady {
		return Problem{}, apperror.NotFound("problem.not_ready", "problem is not ready for judge")
	}
	return Problem{
		ID:                   p.ID,
		Slug:                 p.Slug,
		Title:                p.Title,
		Visibility:           p.Visibility,
		OwnerUserID:          p.OwnerUserID,
		CurrentStatementID:   p.CurrentStatementID,
		CurrentTestcaseSetID: p.CurrentTestcaseSetID,
	}, nil
}

func (s *Service) Stats(ctx context.Context, actor auth.Actor, problemID int64) (ProblemStats, error) {
	p, err := s.repo.GetProblem(ctx, problemID)
	if err != nil {
		return ProblemStats{}, err
	}
	if err := canReadProblem(actor, p); err != nil {
		return ProblemStats{}, err
	}
	stats, err := s.repo.GetProblemStats(ctx, problemID)
	if err != nil {
		return ProblemStats{}, err
	}
	if stats.TotalSubmissions > 0 {
		stats.AcceptanceRate = float64(stats.AcceptedSubmissions) / float64(stats.TotalSubmissions)
	}
	return stats, nil
}

func ensurePublishable(ctx context.Context, repo Repository, problemID int64) error {
	if _, err := repo.GetCurrentProblemStatement(ctx, problemID); err != nil {
		return apperror.Unprocessable("problem.not_publishable", "current statement is required before publishing")
	}
	if _, err := repo.GetCurrentReadyTestcaseSet(ctx, problemID); err != nil {
		return apperror.Unprocessable("problem.not_publishable", "current ready testcase set is required before publishing")
	}
	return nil
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

func readAllAndClose(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	return io.ReadAll(body)
}
