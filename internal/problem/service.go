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
	if filter.Mine && !actor.Authenticated() {
		return ProblemList{}, requireAuthenticated(actor)
	}
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

func (s *Service) GetProblemAuthoringState(ctx context.Context, actor auth.Actor, id int64) (ProblemAuthoringState, error) {
	p, err := s.repo.GetProblem(ctx, id)
	if err != nil {
		return ProblemAuthoringState{}, err
	}
	if err := canWriteProblem(actor, p); err != nil {
		return ProblemAuthoringState{}, err
	}
	response, err := s.ProblemResponse(ctx, p)
	if err != nil {
		return ProblemAuthoringState{}, err
	}
	readiness, err := loadProblemAuthoringReadiness(ctx, s.repo, id)
	if err != nil {
		return ProblemAuthoringState{}, err
	}
	return ProblemAuthoringState{
		Problem:     response,
		Statement:   readiness.statement,
		TestcaseSet: readiness.testcaseSet,
		LatestCheck: readiness.latestCheck,
		Publishable: len(readiness.blockers) == 0,
		Blockers:    readiness.blockers,
	}, nil
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
		if err != nil {
			return err
		}
		return demotePublishedProblem(ctx, repo, p)
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
		if err != nil {
			return err
		}
		return demotePublishedProblem(ctx, repo, p)
	})
	if err != nil {
		_ = s.storage.Delete(ctx, key)
	}
	return created, err
}

func (s *Service) RunProblemCheck(ctx context.Context, actor auth.Actor, problemID int64) (ProblemCheckResult, error) {
	p, err := s.repo.GetProblem(ctx, problemID)
	if err != nil {
		return ProblemCheckResult{}, err
	}
	if err := canWriteProblem(actor, p); err != nil {
		return ProblemCheckResult{}, err
	}

	statement, err := s.repo.GetCurrentProblemStatement(ctx, problemID)
	if err != nil {
		return ProblemCheckResult{}, err
	}
	set, err := s.repo.GetCurrentReadyTestcaseSet(ctx, problemID)
	if err != nil {
		return ProblemCheckResult{}, err
	}

	findings := validateProblemCheckStatementSamples(statement)
	storageReadable := false
	zipReadable := false
	caseCount := 0
	if s.storage == nil {
		findings = append(findings, problemCheckFindingDraft{
			severity: ProblemCheckSeverityError,
			code:     "testcase.storage_unreadable",
			message:  "testcase object storage is unavailable",
			details:  problemCheckDetails(map[string]any{"storage_key": set.StorageKey}),
		})
	} else if strings.TrimSpace(set.StorageKey) == "" {
		findings = append(findings, problemCheckFindingDraft{
			severity: ProblemCheckSeverityError,
			code:     "testcase.storage_unreadable",
			message:  "testcase archive storage key is missing",
		})
	} else {
		body, _, err := s.storage.Get(ctx, set.StorageKey)
		if err != nil {
			findings = append(findings, problemCheckFindingDraft{
				severity: ProblemCheckSeverityError,
				code:     "testcase.storage_unreadable",
				message:  "testcase archive cannot be read from storage",
				details:  problemCheckDetails(map[string]any{"storage_key": set.StorageKey}),
			})
		} else {
			storageReadable = true
			data, err := readAllAndClose(body, defaultMaxTestcaseArchiveBytes)
			if err != nil {
				if resourceErr, ok := err.(*testcaseArchiveResourceError); ok {
					findings = append(findings, problemCheckFindingDraft{
						severity: ProblemCheckSeverityError,
						code:     resourceErr.code,
						message:  resourceErr.message,
						details:  problemCheckDetails(map[string]any{"storage_key": set.StorageKey}),
					})
				} else {
					storageReadable = false
					findings = append(findings, problemCheckFindingDraft{
						severity: ProblemCheckSeverityError,
						code:     "testcase.storage_unreadable",
						message:  "testcase archive cannot be read from storage",
						details:  problemCheckDetails(map[string]any{"storage_key": set.StorageKey}),
					})
				}
			} else {
				archiveResult := validateProblemCheckArchive(data, set)
				zipReadable = archiveResult.zipReadable
				caseCount = archiveResult.caseCount
				findings = append(findings, archiveResult.findings...)
			}
		}
	}

	summary := problemCheckSummary(set.CaseCount, caseCount, storageReadable, zipReadable, findings)
	summaryJSON, err := marshalProblemCheckSummary(summary)
	if err != nil {
		return ProblemCheckResult{}, err
	}

	var runRecord ProblemCheckRunRecord
	persistedFindings := make([]ProblemCheckFinding, 0, len(findings))
	err = s.repo.WithTx(ctx, func(ctx context.Context, repo Repository) error {
		run, err := repo.CreateProblemCheckRun(ctx, CreateProblemCheckRunInput{
			ProblemID:     problemID,
			StatementID:   statement.ID,
			TestcaseSetID: set.ID,
			RequestedBy:   actor.UserID,
			Status:        ProblemCheckStatusRunning,
			Summary:       json.RawMessage(`{}`),
		})
		if err != nil {
			return err
		}
		for _, finding := range findings {
			record, err := repo.CreateProblemCheckFinding(ctx, CreateProblemCheckFindingInput{
				RunID:       run.ID,
				Severity:    finding.severity,
				Code:        finding.code,
				Message:     finding.message,
				CaseIndex:   finding.caseIndex,
				TestcaseKey: finding.testcaseKey,
				Details:     finding.details,
			})
			if err != nil {
				return err
			}
			persistedFindings = append(persistedFindings, serviceProblemCheckFindingFromRecord(record))
		}
		runRecord, err = repo.CompleteProblemCheckRun(ctx, CompleteProblemCheckRunInput{
			ID:         run.ID,
			Summary:    summaryJSON,
			FinishedAt: s.now(),
		})
		return err
	})
	if err != nil {
		return ProblemCheckResult{}, err
	}
	return ProblemCheckResult{Run: serviceProblemCheckRunFromRecord(runRecord), Findings: persistedFindings}, nil
}

func (s *Service) GetProblemCheck(ctx context.Context, actor auth.Actor, problemID int64, checkID int64) (ProblemCheckResult, error) {
	p, err := s.repo.GetProblem(ctx, problemID)
	if err != nil {
		return ProblemCheckResult{}, err
	}
	if err := canWriteProblem(actor, p); err != nil {
		return ProblemCheckResult{}, err
	}
	run, err := s.repo.GetProblemCheckRun(ctx, checkID)
	if err != nil {
		return ProblemCheckResult{}, problemCheckNotFoundErr(err)
	}
	if run.ProblemID != problemID {
		return ProblemCheckResult{}, apperror.NotFound("problem_check.not_found", "problem check not found")
	}
	records, err := s.repo.ListProblemCheckFindings(ctx, checkID)
	if err != nil {
		return ProblemCheckResult{}, err
	}
	findings := make([]ProblemCheckFinding, 0, len(records))
	for _, record := range records {
		findings = append(findings, serviceProblemCheckFindingFromRecord(record))
	}
	return ProblemCheckResult{Run: serviceProblemCheckRunFromRecord(run), Findings: findings}, nil
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
	data, err := readAllAndClose(body, defaultMaxTestcaseArchiveBytes)
	if err != nil {
		if _, ok := err.(*testcaseArchiveResourceError); ok {
			return TestcaseSet{}, testcaseArchiveBadRequest(err)
		}
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

func (s *Service) AuthorizeProblemRejudge(ctx context.Context, actor auth.Actor, id int64) error {
	problem, err := s.repo.GetProblem(ctx, id)
	if err != nil {
		return err
	}
	return canWriteProblem(actor, problem)
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
	readiness, err := loadProblemAuthoringReadiness(ctx, repo, problemID)
	if err != nil {
		return err
	}
	if len(readiness.blockers) > 0 {
		blocker := readiness.blockers[0]
		return apperror.Unprocessable(blocker.Code, blocker.Message)
	}
	return nil
}

func demotePublishedProblem(ctx context.Context, repo Repository, problem ProblemRecord) error {
	if problem.Status != StatusPublished {
		return nil
	}
	status := StatusDraft
	_, err := repo.UpdateProblem(ctx, problem.ID, UpdateProblemInput{Status: &status})
	return err
}

type problemAuthoringReadiness struct {
	statement   *Statement
	testcaseSet *TestcaseSetRecord
	latestCheck *ProblemCheckRun
	blockers    []ProblemAuthoringBlocker
}

func loadProblemAuthoringReadiness(ctx context.Context, repo Repository, problemID int64) (problemAuthoringReadiness, error) {
	state := problemAuthoringReadiness{blockers: []ProblemAuthoringBlocker{}}
	statement, err := repo.GetCurrentProblemStatement(ctx, problemID)
	if err != nil {
		if !isNotFoundError(err) {
			return problemAuthoringReadiness{}, err
		}
		state.blockers = append(state.blockers, ProblemAuthoringBlocker{Code: "problem.statement_required", Message: "current statement is required before publishing"})
	} else {
		state.statement = &statement
	}

	testcaseSet, err := repo.GetCurrentReadyTestcaseSet(ctx, problemID)
	if err != nil {
		if !isNotFoundError(err) {
			return problemAuthoringReadiness{}, err
		}
		state.blockers = append(state.blockers, ProblemAuthoringBlocker{Code: "problem.testcase_required", Message: "current ready testcase set is required before publishing"})
		return state, nil
	}
	state.testcaseSet = &testcaseSet

	if state.statement == nil {
		return state, nil
	}
	runRecord, err := repo.GetLatestCompletedProblemCheckRun(ctx, problemID, state.statement.ID, testcaseSet.ID)
	if err != nil {
		if !isNotFoundError(err) {
			return problemAuthoringReadiness{}, err
		}
		state.blockers = append(state.blockers, ProblemAuthoringBlocker{Code: "problem.check_required", Message: "run a problem check for the current testcase set before publishing"})
		return state, nil
	}
	run := problemCheckRunFromRecord(runRecord)
	findings, err := repo.ListProblemCheckFindings(ctx, run.ID)
	if err != nil {
		return problemAuthoringReadiness{}, err
	}
	run.Findings = make([]ProblemCheckFinding, 0, len(findings))
	for _, finding := range findings {
		run.Findings = append(run.Findings, problemCheckFindingFromRecord(finding))
	}
	state.latestCheck = &run
	if !run.Summary.Valid {
		state.blockers = append(state.blockers, ProblemAuthoringBlocker{Code: "problem.check_failed", Message: "the current testcase set has validation errors"})
	}
	return state, nil
}

func isNotFoundError(err error) bool {
	appErr, ok := apperror.From(err)
	return ok && appErr.HTTPStatus == http.StatusNotFound
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

func serviceProblemCheckRunFromRecord(record ProblemCheckRunRecord) ProblemCheckRun {
	summary := ProblemCheckSummary{}
	if len(record.Summary) > 0 {
		_ = json.Unmarshal(record.Summary, &summary)
	}
	return ProblemCheckRun{
		ID:            record.ID,
		ProblemID:     record.ProblemID,
		StatementID:   record.StatementID,
		TestcaseSetID: record.TestcaseSetID,
		RequestedBy:   record.RequestedBy,
		Status:        record.Status,
		Summary:       summary,
		ErrorMessage:  record.ErrorMessage,
		StartedAt:     record.StartedAt,
		FinishedAt:    record.FinishedAt,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
	}
}

func serviceProblemCheckFindingFromRecord(record ProblemCheckFindingRecord) ProblemCheckFinding {
	details := record.Details
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	return ProblemCheckFinding{
		ID:          record.ID,
		RunID:       record.RunID,
		Severity:    record.Severity,
		Code:        record.Code,
		Message:     record.Message,
		CaseIndex:   record.CaseIndex,
		TestcaseKey: record.TestcaseKey,
		Details:     details,
		CreatedAt:   record.CreatedAt,
	}
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
