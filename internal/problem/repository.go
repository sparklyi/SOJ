package problem

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/postgres"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	WithTx(ctx context.Context, fn func(context.Context, Repository) error) error
	CreateProblem(ctx context.Context, ownerUserID int64, input CreateProblemInput) (ProblemRecord, error)
	GetProblem(ctx context.Context, id int64) (ProblemRecord, error)
	ListProblems(ctx context.Context, filter ListProblemsFilter) ([]ProblemRecord, error)
	CountProblems(ctx context.Context, filter ListProblemsFilter) (int64, error)
	UpdateProblem(ctx context.Context, id int64, input UpdateProblemInput) (ProblemRecord, error)
	ArchiveProblem(ctx context.Context, id int64) (ProblemRecord, error)
	LockProblemForUpdate(ctx context.Context, id int64) (ProblemRecord, error)
	NextProblemStatementVersion(ctx context.Context, problemID int64) (int32, error)
	ClearCurrentProblemStatement(ctx context.Context, problemID int64) error
	CreateProblemStatement(ctx context.Context, problemID int64, version int32, input CreateStatementInput) (Statement, error)
	GetCurrentProblemStatement(ctx context.Context, problemID int64) (Statement, error)
	ReplaceProblemTags(ctx context.Context, problemID int64, tags []TagInput) ([]Tag, error)
	ListProblemTags(ctx context.Context, problemID int64) ([]Tag, error)
	NextTestcaseSetVersion(ctx context.Context, problemID int64) (int32, error)
	ClearCurrentTestcaseSet(ctx context.Context, problemID int64) error
	CreateTestcaseSet(ctx context.Context, problemID int64, version int32, storageKey, checksum string, sizeBytes int64, caseCount int32, createdBy int64) (TestcaseSetRecord, error)
	GetCurrentReadyTestcaseSet(ctx context.Context, problemID int64) (TestcaseSetRecord, error)
	CreateProblemCheckRun(ctx context.Context, input CreateProblemCheckRunInput) (ProblemCheckRunRecord, error)
	GetProblemCheckRun(ctx context.Context, id int64) (ProblemCheckRunRecord, error)
	ListProblemCheckRuns(ctx context.Context, filter ListProblemCheckRunsFilter) ([]ProblemCheckRunRecord, error)
	CompleteProblemCheckRun(ctx context.Context, input CompleteProblemCheckRunInput) (ProblemCheckRunRecord, error)
	FailProblemCheckRun(ctx context.Context, input FailProblemCheckRunInput) (ProblemCheckRunRecord, error)
	CreateProblemCheckFinding(ctx context.Context, input CreateProblemCheckFindingInput) (ProblemCheckFindingRecord, error)
	GetProblemCheckFinding(ctx context.Context, id int64) (ProblemCheckFindingRecord, error)
	ListProblemCheckFindings(ctx context.Context, runID int64) ([]ProblemCheckFindingRecord, error)
	CreateArtifact(ctx context.Context, artifact ArtifactRecord) (ArtifactRecord, error)
	GetProblemStats(ctx context.Context, problemID int64) (ProblemStats, error)
}

type CreateProblemCheckRunInput struct {
	ProblemID     int64
	TestcaseSetID int64
	RequestedBy   int64
	Status        string
	Summary       json.RawMessage
}

type ListProblemCheckRunsFilter struct {
	ProblemID int64
	Offset    int32
	Limit     int32
}

type CompleteProblemCheckRunInput struct {
	ID         int64
	Summary    json.RawMessage
	FinishedAt time.Time
}

type FailProblemCheckRunInput struct {
	ID           int64
	Summary      json.RawMessage
	ErrorMessage string
	FinishedAt   time.Time
}

type CreateProblemCheckFindingInput struct {
	RunID       int64
	Severity    string
	Code        string
	Message     string
	CaseIndex   int32
	TestcaseKey string
	Details     json.RawMessage
}

type ProblemCheckRunRecord struct {
	ID            int64           `json:"id"`
	ProblemID     int64           `json:"problem_id"`
	TestcaseSetID int64           `json:"testcase_set_id,omitempty"`
	RequestedBy   int64           `json:"requested_by,omitempty"`
	Status        string          `json:"status"`
	Summary       json.RawMessage `json:"summary"`
	ErrorMessage  string          `json:"error_message,omitempty"`
	StartedAt     time.Time       `json:"started_at,omitempty"`
	FinishedAt    time.Time       `json:"finished_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at,omitempty"`
	UpdatedAt     time.Time       `json:"updated_at,omitempty"`
}

type ProblemCheckFindingRecord struct {
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

type PostgresRepository struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, queries: db.New(pool)}
}

func (r *PostgresRepository) WithTx(ctx context.Context, fn func(context.Context, Repository) error) error {
	return postgres.WithPoolTx(ctx, r.pool, func(tx pgx.Tx) error {
		return fn(ctx, &txRepository{queries: r.queries.WithTx(tx)})
	})
}

func (r *PostgresRepository) CreateProblem(ctx context.Context, ownerUserID int64, input CreateProblemInput) (ProblemRecord, error) {
	p, err := r.queries.CreateProblem(ctx, db.CreateProblemParams{
		OwnerUserID:   ownerUserID,
		Title:         input.Title,
		Slug:          input.Slug,
		Difficulty:    input.Difficulty,
		Visibility:    input.Visibility,
		Status:        StatusDraft,
		TimeLimitMs:   input.TimeLimitMS,
		MemoryLimitKb: input.MemoryLimitKB,
	})
	return problemFromDB(p), mapDBErr(err)
}

func (r *PostgresRepository) GetProblem(ctx context.Context, id int64) (ProblemRecord, error) {
	p, err := r.queries.GetProblemByID(ctx, id)
	return problemFromGetRow(p), mapDBErr(err)
}

func (r *PostgresRepository) ListProblems(ctx context.Context, filter ListProblemsFilter) ([]ProblemRecord, error) {
	rows, err := r.queries.ListProblems(ctx, db.ListProblemsParams{
		Difficulty:   textArg(filter.Difficulty),
		Status:       textArg(filter.Status),
		Visibility:   textArg(filter.Visibility),
		Tag:          textArg(filter.Tag),
		Keyword:      textArg(filter.Keyword),
		IncludeAll:   filter.IncludeAll,
		ViewerUserID: filter.ViewerUserID,
		Offset:       filter.Offset,
		Limit:        filter.Limit,
	})
	if err != nil {
		return nil, mapDBErr(err)
	}
	items := make([]ProblemRecord, 0, len(rows))
	for _, row := range rows {
		items = append(items, problemFromListRow(row))
	}
	return items, nil
}

func (r *PostgresRepository) CountProblems(ctx context.Context, filter ListProblemsFilter) (int64, error) {
	count, err := r.queries.CountProblems(ctx, db.CountProblemsParams{
		Difficulty:   textArg(filter.Difficulty),
		Status:       textArg(filter.Status),
		Visibility:   textArg(filter.Visibility),
		Tag:          textArg(filter.Tag),
		Keyword:      textArg(filter.Keyword),
		IncludeAll:   filter.IncludeAll,
		ViewerUserID: filter.ViewerUserID,
	})
	return count, mapDBErr(err)
}

func (r *PostgresRepository) UpdateProblem(ctx context.Context, id int64, input UpdateProblemInput) (ProblemRecord, error) {
	return updateProblem(ctx, r.queries, id, input)
}

func (r *PostgresRepository) ArchiveProblem(ctx context.Context, id int64) (ProblemRecord, error) {
	p, err := r.queries.ArchiveProblem(ctx, id)
	return problemFromDB(p), mapDBErr(err)
}

func (r *PostgresRepository) LockProblemForUpdate(ctx context.Context, id int64) (ProblemRecord, error) {
	p, err := r.queries.LockProblemForUpdate(ctx, id)
	return problemFromLockRow(p), mapDBErr(err)
}

func (r *PostgresRepository) NextProblemStatementVersion(ctx context.Context, problemID int64) (int32, error) {
	version, err := r.queries.NextProblemStatementVersion(ctx, problemID)
	return version, mapDBErr(err)
}

func (r *PostgresRepository) ClearCurrentProblemStatement(ctx context.Context, problemID int64) error {
	return mapDBErr(r.queries.ClearCurrentProblemStatement(ctx, problemID))
}

func (r *PostgresRepository) CreateProblemStatement(ctx context.Context, problemID int64, version int32, input CreateStatementInput) (Statement, error) {
	return createProblemStatement(ctx, r.queries, problemID, version, input)
}

func (r *PostgresRepository) GetCurrentProblemStatement(ctx context.Context, problemID int64) (Statement, error) {
	statement, err := r.queries.GetCurrentProblemStatement(ctx, problemID)
	return statementFromDB(statement), mapDBErr(err)
}

func (r *PostgresRepository) ReplaceProblemTags(ctx context.Context, problemID int64, tags []TagInput) ([]Tag, error) {
	return replaceProblemTags(ctx, r.queries, problemID, tags)
}

func (r *PostgresRepository) ListProblemTags(ctx context.Context, problemID int64) ([]Tag, error) {
	return listProblemTags(ctx, r.queries, problemID)
}

func (r *PostgresRepository) NextTestcaseSetVersion(ctx context.Context, problemID int64) (int32, error) {
	version, err := r.queries.NextTestcaseSetVersion(ctx, problemID)
	return version, mapDBErr(err)
}

func (r *PostgresRepository) ClearCurrentTestcaseSet(ctx context.Context, problemID int64) error {
	return mapDBErr(r.queries.ClearCurrentTestcaseSet(ctx, problemID))
}

func (r *PostgresRepository) CreateTestcaseSet(ctx context.Context, problemID int64, version int32, storageKey, checksum string, sizeBytes int64, caseCount int32, createdBy int64) (TestcaseSetRecord, error) {
	return createTestcaseSet(ctx, r.queries, problemID, version, storageKey, checksum, sizeBytes, caseCount, createdBy)
}

func (r *PostgresRepository) GetCurrentReadyTestcaseSet(ctx context.Context, problemID int64) (TestcaseSetRecord, error) {
	set, err := r.queries.GetCurrentReadyTestcaseSet(ctx, problemID)
	return testcaseSetFromDB(set), mapDBErr(err)
}

func (r *PostgresRepository) CreateProblemCheckRun(ctx context.Context, input CreateProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	return createProblemCheckRun(ctx, r.queries, input)
}

func (r *PostgresRepository) GetProblemCheckRun(ctx context.Context, id int64) (ProblemCheckRunRecord, error) {
	run, err := r.queries.GetProblemCheckRunByID(ctx, id)
	return problemCheckRunFromDB(run), mapDBErr(err)
}

func (r *PostgresRepository) ListProblemCheckRuns(ctx context.Context, filter ListProblemCheckRunsFilter) ([]ProblemCheckRunRecord, error) {
	return listProblemCheckRuns(ctx, r.queries, filter)
}

func (r *PostgresRepository) CompleteProblemCheckRun(ctx context.Context, input CompleteProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	return completeProblemCheckRun(ctx, r.queries, input)
}

func (r *PostgresRepository) FailProblemCheckRun(ctx context.Context, input FailProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	return failProblemCheckRun(ctx, r.queries, input)
}

func (r *PostgresRepository) CreateProblemCheckFinding(ctx context.Context, input CreateProblemCheckFindingInput) (ProblemCheckFindingRecord, error) {
	return createProblemCheckFinding(ctx, r.queries, input)
}

func (r *PostgresRepository) GetProblemCheckFinding(ctx context.Context, id int64) (ProblemCheckFindingRecord, error) {
	finding, err := r.queries.GetProblemCheckFindingByID(ctx, id)
	return problemCheckFindingFromDB(finding), mapDBErr(err)
}

func (r *PostgresRepository) ListProblemCheckFindings(ctx context.Context, runID int64) ([]ProblemCheckFindingRecord, error) {
	return listProblemCheckFindings(ctx, r.queries, runID)
}

func (r *PostgresRepository) CreateArtifact(ctx context.Context, artifact ArtifactRecord) (ArtifactRecord, error) {
	return createArtifact(ctx, r.queries, artifact)
}

func (r *PostgresRepository) GetProblemStats(ctx context.Context, problemID int64) (ProblemStats, error) {
	stats, err := r.queries.GetProblemStats(ctx, problemID)
	return statsFromDB(stats), mapDBErr(err)
}

type txRepository struct {
	queries *db.Queries
}

func (r *txRepository) WithTx(ctx context.Context, fn func(context.Context, Repository) error) error {
	return fn(ctx, r)
}

func (r *txRepository) CreateProblem(ctx context.Context, ownerUserID int64, input CreateProblemInput) (ProblemRecord, error) {
	p, err := r.queries.CreateProblem(ctx, db.CreateProblemParams{
		OwnerUserID:   ownerUserID,
		Title:         input.Title,
		Slug:          input.Slug,
		Difficulty:    input.Difficulty,
		Visibility:    input.Visibility,
		Status:        StatusDraft,
		TimeLimitMs:   input.TimeLimitMS,
		MemoryLimitKb: input.MemoryLimitKB,
	})
	return problemFromDB(p), mapDBErr(err)
}

func (r *txRepository) GetProblem(ctx context.Context, id int64) (ProblemRecord, error) {
	p, err := r.queries.GetProblemByID(ctx, id)
	return problemFromGetRow(p), mapDBErr(err)
}

func (r *txRepository) ListProblems(ctx context.Context, filter ListProblemsFilter) ([]ProblemRecord, error) {
	rows, err := r.queries.ListProblems(ctx, db.ListProblemsParams{
		Difficulty:   textArg(filter.Difficulty),
		Status:       textArg(filter.Status),
		Visibility:   textArg(filter.Visibility),
		Tag:          textArg(filter.Tag),
		Keyword:      textArg(filter.Keyword),
		IncludeAll:   filter.IncludeAll,
		ViewerUserID: filter.ViewerUserID,
		Offset:       filter.Offset,
		Limit:        filter.Limit,
	})
	if err != nil {
		return nil, mapDBErr(err)
	}
	items := make([]ProblemRecord, 0, len(rows))
	for _, row := range rows {
		items = append(items, problemFromListRow(row))
	}
	return items, nil
}

func (r *txRepository) CountProblems(ctx context.Context, filter ListProblemsFilter) (int64, error) {
	count, err := r.queries.CountProblems(ctx, db.CountProblemsParams{
		Difficulty:   textArg(filter.Difficulty),
		Status:       textArg(filter.Status),
		Visibility:   textArg(filter.Visibility),
		Tag:          textArg(filter.Tag),
		Keyword:      textArg(filter.Keyword),
		IncludeAll:   filter.IncludeAll,
		ViewerUserID: filter.ViewerUserID,
	})
	return count, mapDBErr(err)
}

func (r *txRepository) UpdateProblem(ctx context.Context, id int64, input UpdateProblemInput) (ProblemRecord, error) {
	return updateProblem(ctx, r.queries, id, input)
}

func (r *txRepository) ArchiveProblem(ctx context.Context, id int64) (ProblemRecord, error) {
	p, err := r.queries.ArchiveProblem(ctx, id)
	return problemFromDB(p), mapDBErr(err)
}

func (r *txRepository) LockProblemForUpdate(ctx context.Context, id int64) (ProblemRecord, error) {
	p, err := r.queries.LockProblemForUpdate(ctx, id)
	return problemFromLockRow(p), mapDBErr(err)
}

func (r *txRepository) NextProblemStatementVersion(ctx context.Context, problemID int64) (int32, error) {
	version, err := r.queries.NextProblemStatementVersion(ctx, problemID)
	return version, mapDBErr(err)
}

func (r *txRepository) ClearCurrentProblemStatement(ctx context.Context, problemID int64) error {
	return mapDBErr(r.queries.ClearCurrentProblemStatement(ctx, problemID))
}

func (r *txRepository) CreateProblemStatement(ctx context.Context, problemID int64, version int32, input CreateStatementInput) (Statement, error) {
	return createProblemStatement(ctx, r.queries, problemID, version, input)
}

func (r *txRepository) GetCurrentProblemStatement(ctx context.Context, problemID int64) (Statement, error) {
	statement, err := r.queries.GetCurrentProblemStatement(ctx, problemID)
	return statementFromDB(statement), mapDBErr(err)
}

func (r *txRepository) ReplaceProblemTags(ctx context.Context, problemID int64, tags []TagInput) ([]Tag, error) {
	return replaceProblemTags(ctx, r.queries, problemID, tags)
}

func (r *txRepository) ListProblemTags(ctx context.Context, problemID int64) ([]Tag, error) {
	return listProblemTags(ctx, r.queries, problemID)
}

func (r *txRepository) NextTestcaseSetVersion(ctx context.Context, problemID int64) (int32, error) {
	version, err := r.queries.NextTestcaseSetVersion(ctx, problemID)
	return version, mapDBErr(err)
}

func (r *txRepository) ClearCurrentTestcaseSet(ctx context.Context, problemID int64) error {
	return mapDBErr(r.queries.ClearCurrentTestcaseSet(ctx, problemID))
}

func (r *txRepository) CreateTestcaseSet(ctx context.Context, problemID int64, version int32, storageKey, checksum string, sizeBytes int64, caseCount int32, createdBy int64) (TestcaseSetRecord, error) {
	return createTestcaseSet(ctx, r.queries, problemID, version, storageKey, checksum, sizeBytes, caseCount, createdBy)
}

func (r *txRepository) GetCurrentReadyTestcaseSet(ctx context.Context, problemID int64) (TestcaseSetRecord, error) {
	set, err := r.queries.GetCurrentReadyTestcaseSet(ctx, problemID)
	return testcaseSetFromDB(set), mapDBErr(err)
}

func (r *txRepository) CreateProblemCheckRun(ctx context.Context, input CreateProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	return createProblemCheckRun(ctx, r.queries, input)
}

func (r *txRepository) GetProblemCheckRun(ctx context.Context, id int64) (ProblemCheckRunRecord, error) {
	run, err := r.queries.GetProblemCheckRunByID(ctx, id)
	return problemCheckRunFromDB(run), mapDBErr(err)
}

func (r *txRepository) ListProblemCheckRuns(ctx context.Context, filter ListProblemCheckRunsFilter) ([]ProblemCheckRunRecord, error) {
	return listProblemCheckRuns(ctx, r.queries, filter)
}

func (r *txRepository) CompleteProblemCheckRun(ctx context.Context, input CompleteProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	return completeProblemCheckRun(ctx, r.queries, input)
}

func (r *txRepository) FailProblemCheckRun(ctx context.Context, input FailProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	return failProblemCheckRun(ctx, r.queries, input)
}

func (r *txRepository) CreateProblemCheckFinding(ctx context.Context, input CreateProblemCheckFindingInput) (ProblemCheckFindingRecord, error) {
	return createProblemCheckFinding(ctx, r.queries, input)
}

func (r *txRepository) GetProblemCheckFinding(ctx context.Context, id int64) (ProblemCheckFindingRecord, error) {
	finding, err := r.queries.GetProblemCheckFindingByID(ctx, id)
	return problemCheckFindingFromDB(finding), mapDBErr(err)
}

func (r *txRepository) ListProblemCheckFindings(ctx context.Context, runID int64) ([]ProblemCheckFindingRecord, error) {
	return listProblemCheckFindings(ctx, r.queries, runID)
}

func (r *txRepository) CreateArtifact(ctx context.Context, artifact ArtifactRecord) (ArtifactRecord, error) {
	return createArtifact(ctx, r.queries, artifact)
}

func (r *txRepository) GetProblemStats(ctx context.Context, problemID int64) (ProblemStats, error) {
	stats, err := r.queries.GetProblemStats(ctx, problemID)
	return statsFromDB(stats), mapDBErr(err)
}

func updateProblem(ctx context.Context, q *db.Queries, id int64, input UpdateProblemInput) (ProblemRecord, error) {
	p, err := q.UpdateProblem(ctx, db.UpdateProblemParams{
		Title:         textPtr(input.Title),
		Slug:          textPtr(input.Slug),
		Difficulty:    textPtr(input.Difficulty),
		Visibility:    textPtr(input.Visibility),
		Status:        textPtr(input.Status),
		TimeLimitMs:   int4Ptr(input.TimeLimitMS),
		MemoryLimitKb: int4Ptr(input.MemoryLimitKB),
		ID:            id,
	})
	return problemFromDB(p), mapDBErr(err)
}

func createProblemStatement(ctx context.Context, q *db.Queries, problemID int64, version int32, input CreateStatementInput) (Statement, error) {
	samples := input.Samples
	if len(samples) == 0 {
		samples = []byte("[]")
	}
	statement, err := q.CreateProblemStatement(ctx, db.CreateProblemStatementParams{
		ProblemID:         problemID,
		Version:           version,
		Title:             input.Title,
		Description:       input.Description,
		InputDescription:  textValue(input.InputDescription),
		OutputDescription: textValue(input.OutputDescription),
		Samples:           samples,
		Hint:              textValue(input.Hint),
		Source:            textValue(input.Source),
		IsCurrent:         input.MakeCurrent,
	})
	return statementFromDB(statement), mapDBErr(err)
}

func replaceProblemTags(ctx context.Context, q *db.Queries, problemID int64, inputs []TagInput) ([]Tag, error) {
	if err := q.ClearProblemTags(ctx, problemID); err != nil {
		return nil, mapDBErr(err)
	}
	tags := make([]Tag, 0, len(inputs))
	for _, input := range inputs {
		tag, err := q.CreateProblemTag(ctx, db.CreateProblemTagParams{Name: input.Name, Slug: input.Slug})
		if err != nil {
			return nil, mapDBErr(err)
		}
		if err := q.LinkProblemTag(ctx, db.LinkProblemTagParams{ProblemID: problemID, TagID: tag.ID}); err != nil {
			return nil, mapDBErr(err)
		}
		tags = append(tags, Tag{ID: tag.ID, Name: tag.Name, Slug: tag.Slug})
	}
	return tags, nil
}

func listProblemTags(ctx context.Context, q *db.Queries, problemID int64) ([]Tag, error) {
	rows, err := q.ListProblemTags(ctx, problemID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	tags := make([]Tag, 0, len(rows))
	for _, row := range rows {
		tags = append(tags, Tag{ID: row.ID, Name: row.Name, Slug: row.Slug})
	}
	return tags, nil
}

func createTestcaseSet(ctx context.Context, q *db.Queries, problemID int64, version int32, storageKey, checksum string, sizeBytes int64, caseCount int32, createdBy int64) (TestcaseSetRecord, error) {
	set, err := q.CreateTestcaseSet(ctx, db.CreateTestcaseSetParams{
		ProblemID:      problemID,
		Version:        version,
		StorageKey:     storageKey,
		ChecksumSha256: checksum,
		SizeBytes:      sizeBytes,
		CaseCount:      caseCount,
		Status:         TestcaseStatusReady,
		IsCurrent:      true,
		CreatedBy:      createdBy,
	})
	return testcaseSetFromDB(set), mapDBErr(err)
}

func createProblemCheckRun(ctx context.Context, q *db.Queries, input CreateProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	status := input.Status
	if status == "" {
		status = ProblemCheckStatusQueued
	}
	run, err := q.CreateProblemCheckRun(ctx, db.CreateProblemCheckRunParams{
		ProblemID:     input.ProblemID,
		TestcaseSetID: int8Value(input.TestcaseSetID),
		RequestedBy:   int8Value(input.RequestedBy),
		Status:        status,
		Summary:       jsonbArg(input.Summary),
	})
	return problemCheckRunFromDB(run), mapDBErr(err)
}

func listProblemCheckRuns(ctx context.Context, q *db.Queries, filter ListProblemCheckRunsFilter) ([]ProblemCheckRunRecord, error) {
	rows, err := q.ListProblemCheckRunsByProblemID(ctx, db.ListProblemCheckRunsByProblemIDParams{
		ProblemID: filter.ProblemID,
		Offset:    filter.Offset,
		Limit:     filter.Limit,
	})
	if err != nil {
		return nil, mapDBErr(err)
	}
	runs := make([]ProblemCheckRunRecord, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, problemCheckRunFromDB(row))
	}
	return runs, nil
}

func completeProblemCheckRun(ctx context.Context, q *db.Queries, input CompleteProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	run, err := q.CompleteProblemCheckRun(ctx, db.CompleteProblemCheckRunParams{
		ID:         input.ID,
		Summary:    jsonbArg(input.Summary),
		FinishedAt: timeArg(input.FinishedAt),
	})
	return problemCheckRunFromDB(run), mapDBErr(err)
}

func failProblemCheckRun(ctx context.Context, q *db.Queries, input FailProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	run, err := q.FailProblemCheckRun(ctx, db.FailProblemCheckRunParams{
		ID:           input.ID,
		Summary:      jsonbArg(input.Summary),
		ErrorMessage: textValue(input.ErrorMessage),
		FinishedAt:   timeArg(input.FinishedAt),
	})
	return problemCheckRunFromDB(run), mapDBErr(err)
}

func createProblemCheckFinding(ctx context.Context, q *db.Queries, input CreateProblemCheckFindingInput) (ProblemCheckFindingRecord, error) {
	finding, err := q.CreateProblemCheckFinding(ctx, db.CreateProblemCheckFindingParams{
		RunID:       input.RunID,
		Severity:    input.Severity,
		Code:        input.Code,
		Message:     input.Message,
		CaseIndex:   int4Value(input.CaseIndex),
		TestcaseKey: textValue(input.TestcaseKey),
		Details:     jsonbArg(input.Details),
	})
	return problemCheckFindingFromDB(finding), mapDBErr(err)
}

func listProblemCheckFindings(ctx context.Context, q *db.Queries, runID int64) ([]ProblemCheckFindingRecord, error) {
	rows, err := q.ListProblemCheckFindingsByRunID(ctx, runID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	findings := make([]ProblemCheckFindingRecord, 0, len(rows))
	for _, row := range rows {
		findings = append(findings, problemCheckFindingFromDB(row))
	}
	return findings, nil
}

func createArtifact(ctx context.Context, q *db.Queries, artifact ArtifactRecord) (ArtifactRecord, error) {
	created, err := q.CreateArtifact(ctx, db.CreateArtifactParams{
		OwnerType:      artifact.OwnerType,
		OwnerID:        artifact.OwnerID,
		Kind:           artifact.Kind,
		StorageKey:     artifact.StorageKey,
		ChecksumSha256: artifact.ChecksumSHA256,
		SizeBytes:      artifact.SizeBytes,
		ContentType:    artifact.ContentType,
	})
	if err != nil {
		return ArtifactRecord{}, mapDBErr(err)
	}
	artifact.ID = created.ID
	return artifact, nil
}

func problemFromDB(p db.Problem) ProblemRecord {
	return ProblemRecord{
		ID:            p.ID,
		OwnerUserID:   p.OwnerUserID,
		Title:         p.Title,
		Slug:          p.Slug,
		Difficulty:    p.Difficulty,
		Visibility:    p.Visibility,
		Status:        p.Status,
		TimeLimitMS:   p.TimeLimitMs,
		MemoryLimitKB: p.MemoryLimitKb,
		CreatedAt:     p.CreatedAt.Time,
		UpdatedAt:     p.UpdatedAt.Time,
		PublishedAt:   p.PublishedAt.Time,
	}
}

func problemFromGetRow(p db.GetProblemByIDRow) ProblemRecord {
	return ProblemRecord{
		ID:                    p.ID,
		OwnerUserID:           p.OwnerUserID,
		Title:                 p.Title,
		Slug:                  p.Slug,
		Difficulty:            p.Difficulty,
		Visibility:            p.Visibility,
		Status:                p.Status,
		TimeLimitMS:           p.TimeLimitMs,
		MemoryLimitKB:         p.MemoryLimitKb,
		CurrentStatementID:    p.CurrentStatementID,
		CurrentTestcaseSetID:  p.CurrentTestcaseSetID,
		CurrentTestcaseStatus: p.CurrentTestcaseStatus,
		CreatedAt:             p.CreatedAt.Time,
		UpdatedAt:             p.UpdatedAt.Time,
		PublishedAt:           p.PublishedAt.Time,
	}
}

func problemFromListRow(p db.ListProblemsRow) ProblemRecord {
	return ProblemRecord{
		ID:                    p.ID,
		OwnerUserID:           p.OwnerUserID,
		Title:                 p.Title,
		Slug:                  p.Slug,
		Difficulty:            p.Difficulty,
		Visibility:            p.Visibility,
		Status:                p.Status,
		TimeLimitMS:           p.TimeLimitMs,
		MemoryLimitKB:         p.MemoryLimitKb,
		CurrentStatementID:    p.CurrentStatementID,
		CurrentTestcaseSetID:  p.CurrentTestcaseSetID,
		CurrentTestcaseStatus: p.CurrentTestcaseStatus,
		CreatedAt:             p.CreatedAt.Time,
		UpdatedAt:             p.UpdatedAt.Time,
		PublishedAt:           p.PublishedAt.Time,
	}
}

func problemFromLockRow(p db.LockProblemForUpdateRow) ProblemRecord {
	return ProblemRecord{
		ID:                    p.ID,
		OwnerUserID:           p.OwnerUserID,
		Title:                 p.Title,
		Slug:                  p.Slug,
		Difficulty:            p.Difficulty,
		Visibility:            p.Visibility,
		Status:                p.Status,
		TimeLimitMS:           p.TimeLimitMs,
		MemoryLimitKB:         p.MemoryLimitKb,
		CurrentStatementID:    p.CurrentStatementID,
		CurrentTestcaseSetID:  p.CurrentTestcaseSetID,
		CurrentTestcaseStatus: p.CurrentTestcaseStatus,
		CreatedAt:             p.CreatedAt.Time,
		UpdatedAt:             p.UpdatedAt.Time,
		PublishedAt:           p.PublishedAt.Time,
	}
}

func statementFromDB(s db.ProblemStatement) Statement {
	return Statement{
		ID:                s.ID,
		ProblemID:         s.ProblemID,
		Version:           s.Version,
		Title:             s.Title,
		Description:       s.Description,
		InputDescription:  s.InputDescription.String,
		OutputDescription: s.OutputDescription.String,
		Samples:           json.RawMessage(s.Samples),
		Hint:              s.Hint.String,
		Source:            s.Source.String,
		IsCurrent:         s.IsCurrent,
		CreatedAt:         s.CreatedAt.Time,
	}
}

func testcaseSetFromDB(set db.TestcaseSet) TestcaseSetRecord {
	return TestcaseSetRecord{
		ID:             set.ID,
		ProblemID:      set.ProblemID,
		Version:        set.Version,
		StorageKey:     set.StorageKey,
		ChecksumSHA256: set.ChecksumSha256,
		SizeBytes:      set.SizeBytes,
		CaseCount:      set.CaseCount,
		Status:         set.Status,
		IsCurrent:      set.IsCurrent,
		CreatedBy:      set.CreatedBy,
		CreatedAt:      set.CreatedAt.Time,
	}
}

func problemCheckRunFromDB(run db.ProblemCheckRun) ProblemCheckRunRecord {
	return ProblemCheckRunRecord{
		ID:            run.ID,
		ProblemID:     run.ProblemID,
		TestcaseSetID: int8FromDB(run.TestcaseSetID),
		RequestedBy:   int8FromDB(run.RequestedBy),
		Status:        run.Status,
		Summary:       jsonRawFromDB(run.Summary),
		ErrorMessage:  textFromDB(run.ErrorMessage),
		StartedAt:     timeFromDB(run.StartedAt),
		FinishedAt:    timeFromDB(run.FinishedAt),
		CreatedAt:     timeFromDB(run.CreatedAt),
		UpdatedAt:     timeFromDB(run.UpdatedAt),
	}
}

func problemCheckFindingFromDB(finding db.ProblemCheckFinding) ProblemCheckFindingRecord {
	return ProblemCheckFindingRecord{
		ID:          finding.ID,
		RunID:       finding.RunID,
		Severity:    finding.Severity,
		Code:        finding.Code,
		Message:     finding.Message,
		CaseIndex:   int4FromDB(finding.CaseIndex),
		TestcaseKey: textFromDB(finding.TestcaseKey),
		Details:     jsonRawFromDB(finding.Details),
		CreatedAt:   timeFromDB(finding.CreatedAt),
	}
}

func problemCheckRunFromRecord(record ProblemCheckRunRecord) ProblemCheckRun {
	summary := ProblemCheckSummary{}
	if len(record.Summary) > 0 {
		_ = json.Unmarshal(record.Summary, &summary)
	}
	return ProblemCheckRun{
		ID:            record.ID,
		ProblemID:     record.ProblemID,
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

func problemCheckFindingFromRecord(record ProblemCheckFindingRecord) ProblemCheckFinding {
	return ProblemCheckFinding{
		ID:          record.ID,
		RunID:       record.RunID,
		Severity:    record.Severity,
		Code:        record.Code,
		Message:     record.Message,
		CaseIndex:   record.CaseIndex,
		TestcaseKey: record.TestcaseKey,
		Details:     jsonRawFromDB(record.Details),
		CreatedAt:   record.CreatedAt,
	}
}

func statsFromDB(row db.GetProblemStatsRow) ProblemStats {
	stats := ProblemStats{
		ProblemID:           row.ProblemID,
		TotalSubmissions:    row.TotalSubmissions,
		AcceptedSubmissions: row.AcceptedSubmissions,
		StatusCounts:        map[string]int64{},
	}
	switch counts := row.StatusCounts.(type) {
	case []byte:
		_ = json.Unmarshal(counts, &stats.StatusCounts)
	case string:
		_ = json.Unmarshal([]byte(counts), &stats.StatusCounts)
	case map[string]any:
		for key, value := range counts {
			if n, ok := value.(float64); ok {
				stats.StatusCounts[key] = int64(n)
			}
		}
	}
	return stats
}

func textArg(value string) pgtype.Text {
	if strings := stringsTrim(value); strings != "" {
		return pgtype.Text{String: strings, Valid: true}
	}
	return pgtype.Text{}
}

func textPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return textValue(*value)
}

func textValue(value string) pgtype.Text {
	if trimmed := stringsTrim(value); trimmed != "" {
		return pgtype.Text{String: trimmed, Valid: true}
	}
	return pgtype.Text{}
}

func int4Ptr(value *int32) pgtype.Int4 {
	if value == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *value, Valid: true}
}

func int4Value(value int32) pgtype.Int4 {
	if value <= 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: value, Valid: true}
}

func int4FromDB(value pgtype.Int4) int32 {
	if !value.Valid {
		return 0
	}
	return value.Int32
}

func int8Value(value int64) pgtype.Int8 {
	if value <= 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: value, Valid: true}
}

func int8FromDB(value pgtype.Int8) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func timeArg(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func timeFromDB(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

func textFromDB(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func jsonbArg(value json.RawMessage) []byte {
	if len(value) == 0 {
		return []byte("{}")
	}
	return append([]byte(nil), value...)
}

func jsonRawFromDB(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("{}")
	}
	return append(json.RawMessage(nil), value...)
}

func stringsTrim(value string) string {
	return strings.TrimSpace(value)
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if err == pgx.ErrNoRows {
		return apperror.NotFound("problem.not_found", "problem not found")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return apperror.Conflict("problem.slug_conflict", "problem slug already exists")
	}
	return err
}
