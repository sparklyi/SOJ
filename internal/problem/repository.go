package problem

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

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
	CreateArtifact(ctx context.Context, artifact ArtifactRecord) (ArtifactRecord, error)
	GetProblemStats(ctx context.Context, problemID int64) (ProblemStats, error)
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
