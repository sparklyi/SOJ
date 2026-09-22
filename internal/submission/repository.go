package submission

import (
	"context"
	"errors"
	"strconv"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/judge"
	"SOJ/internal/postgres"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type ArtifactRecord struct {
	ID             int64
	OwnerType      string
	OwnerID        int64
	Kind           string
	StorageKey     string
	ChecksumSHA256 string
	SizeBytes      int64
	ContentType    string
	// CreatedAt is when the row was written, and is what the orphan sweep
	// compares against the retention window. It matters: an artifact is written
	// just before the run that will reference it, so for a moment every run
	// artifact is an orphan, and only age distinguishes that from a real one.
	CreatedAt time.Time
}

type SubmissionRecord struct {
	ID               int64
	UserID           int64
	ProblemID        int64
	ContestID        *int64
	LanguageID       int64
	TestcaseSetID    int64
	Status           string
	SourceArtifactID int64
	TimeMS           *int32
	MemoryKB         *int32
	Score            int32
	ErrorMessage     *string
	SubmittedAt      time.Time
	JudgedAt         *time.Time
	FirstJudgedAt    *time.Time
	UpdatedAt        time.Time
}

type RunRecord struct {
	ID     int64
	UserID int64
	// ProblemID is nil for a playground run -- scratch code with no problem
	// attached. A problem-attached run keeps its problem so the problem page can
	// still attribute it. Derived from the column, not a separate discriminator:
	// a `kind` column would be a second source of truth for the same fact.
	ProblemID        *int64
	LanguageID       int64
	Status           string
	SourceArtifactID int64
	Stdin            string
	Stdout           string
	Stderr           string
	CompileOutput    string
	TimeMS           *int32
	MemoryKB         *int32
	ErrorMessage     *string
	CreatedAt        time.Time
	FinishedAt       *time.Time
	UpdatedAt        time.Time
}

type JudgeTaskRecord struct {
	ID int64
	// SubmissionID and RunID are the task's subject: exactly one is non-nil,
	// which the judge_tasks CHECK constraint enforces. Two optional fields rather
	// than one id plus a kind flag -- the flag would be a second source of truth
	// for something the ids already say.
	SubmissionID *int64
	RunID        *int64
	StreamID     string
	Status       string
	Attempts     int32
	NextRunAt    time.Time
	LastError    string
}

type LanguageRecord struct {
	ID               int64
	Engine           string
	EngineLanguageID string
	Name             string
	DefaultTimeLimit time.Duration
	DefaultMemoryKB  int64
	Enabled          bool
}

type JudgeAttemptRecord struct {
	ID                   int64
	SubmissionID         *int64
	RunID                *int64
	TaskID               *int64
	RejudgeBatchID       *int64
	AttemptNo            int32
	ProtocolVersion      string
	JudgeCoreVersion     string
	JudgeEngine          string
	JudgeAgentID         *string
	LanguageID           int64
	LanguageRuntime      *string
	SandboxBackend       *string
	SandboxProfile       *string
	TestcaseSetID        *int64
	TestcaseSetHash      *string
	CheckerHash          *string
	ValidatorHash        *string
	Status               string
	Verdict              *string
	Score                int32
	TimeMS               *int32
	MemoryKB             *int32
	FirstFailedCaseIndex *int32
	FirstFailedGroup     *string
	CompileOutputSummary *string
	StderrSummary        *string
	CheckerMessage       *string
	ErrorClass           *string
	ErrorMessage         *string
	Manifest             []byte
	Metrics              []byte
	TraceID              *string
	StartedAt            *time.Time
	FinishedAt           *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type JudgeCaseResultRecord struct {
	ID                int64
	AttemptID         int64
	CaseIndex         int32
	GroupName         *string
	TestcaseKey       *string
	Status            string
	Score             int32
	TimeMS            *int32
	MemoryKB          *int32
	ExitCode          *int32
	Signal            *string
	CheckerMessage    *string
	OutputDiffSummary *string
	StdoutArtifactID  *int64
	StderrArtifactID  *int64
	DiffArtifactID    *int64
	CreatedAt         time.Time
}

type SubmissionResultRecord struct {
	SubmissionID         int64
	AttemptID            int64
	Status               string
	Score                int32
	TimeMS               *int32
	MemoryKB             *int32
	FirstFailedCaseIndex *int32
	FirstFailedGroup     *string
	ErrorClass           *string
	SafeSummary          []byte
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type ListSubmissionsInput struct {
	UserID    *int64
	ProblemID *int64
	ContestID *int64
	Status    *string
	Offset    int32
	Limit     int32
	Cursor    *SubmissionCursor
}

type SubmissionCursor struct {
	SubmittedAt time.Time
	ID          int64
}

type SubmissionListSummary struct {
	Result        *SubmissionResultRecord
	LatestAttempt *JudgeAttemptRecord
}

type SQLRepository struct {
	q        *db.Queries
	txRunner postgres.TxRunner
}

func NewSQLRepository(q *db.Queries) *SQLRepository {
	return &SQLRepository{q: q}
}

func NewSQLRepositoryWithTxRunner(q *db.Queries, txRunner postgres.TxRunner) *SQLRepository {
	return &SQLRepository{q: q, txRunner: txRunner}
}

func (r *SQLRepository) CreateArtifact(ctx context.Context, arg ArtifactRecord) (ArtifactRecord, error) {
	row, err := r.q.CreateArtifact(ctx, db.CreateArtifactParams{
		OwnerType:      arg.OwnerType,
		OwnerID:        arg.OwnerID,
		Kind:           arg.Kind,
		StorageKey:     arg.StorageKey,
		ChecksumSha256: arg.ChecksumSHA256,
		SizeBytes:      arg.SizeBytes,
		ContentType:    arg.ContentType,
	})
	return artifactRecord(row), err
}

func (r *SQLRepository) GetArtifact(ctx context.Context, id int64) (ArtifactRecord, error) {
	row, err := r.q.GetArtifactByID(ctx, id)
	return artifactRecord(row), mapNotFound(err, "artifact.not_found", "artifact not found")
}

func (r *SQLRepository) GetEnabledLanguage(ctx context.Context, id int64) (LanguageRecord, error) {
	row, err := r.q.GetEnabledLanguageByID(ctx, id)
	return languageRecord(row), mapNotFound(err, "submission.language_disabled", "language is disabled or not found")
}

func (r *SQLRepository) ListLanguages(ctx context.Context, arg ListLanguagesInput) ([]LanguageRecord, int64, error) {
	params := db.ListLanguagesParams{Enabled: boolPtr(arg.Enabled), Engine: textPtr(arg.Engine), Offset: arg.Offset, Limit: arg.Limit}
	rows, err := r.q.ListLanguages(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	total, err := r.q.CountLanguages(ctx, db.CountLanguagesParams{Enabled: params.Enabled, Engine: params.Engine})
	if err != nil {
		return nil, 0, err
	}
	out := make([]LanguageRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, languageRecord(row))
	}
	return out, total, nil
}

func (r *SQLRepository) UpsertLanguage(ctx context.Context, language judge.Language) (LanguageRecord, error) {
	row, err := r.q.UpsertLanguage(ctx, db.UpsertLanguageParams{
		Engine:               judge.EngineSOJAgent,
		EngineLanguageID:     int64String(language.ID),
		Name:                 language.Name,
		DefaultTimeLimitMs:   int32(language.TimeLimit / time.Millisecond),
		DefaultMemoryLimitKb: int32(language.MemoryKB),
		Enabled:              language.Enabled,
	})
	return languageRecord(row), err
}

func (r *SQLRepository) UpdateLanguage(ctx context.Context, id int64, arg UpdateLanguageInput) (LanguageRecord, error) {
	row, err := r.q.UpdateLanguageAdminFields(ctx, db.UpdateLanguageAdminFieldsParams{
		ID:                   id,
		Enabled:              boolPtr(arg.Enabled),
		DefaultTimeLimitMs:   int4Ptr(arg.DefaultTimeLimitMS),
		DefaultMemoryLimitKb: int4Ptr(arg.DefaultMemoryLimitKB),
	})
	return languageRecord(row), mapNotFound(err, "submission.language_disabled", "language is disabled or not found")
}

func artifactRecord(row db.Artifact) ArtifactRecord {
	return ArtifactRecord{ID: row.ID, OwnerType: row.OwnerType, OwnerID: row.OwnerID, Kind: row.Kind, StorageKey: row.StorageKey, ChecksumSHA256: row.ChecksumSha256, SizeBytes: row.SizeBytes, ContentType: row.ContentType, CreatedAt: row.CreatedAt.Time}
}

func languageRecord(row db.Language) LanguageRecord {
	return LanguageRecord{ID: row.ID, Engine: row.Engine, EngineLanguageID: row.EngineLanguageID, Name: row.Name, DefaultTimeLimit: time.Duration(row.DefaultTimeLimitMs) * time.Millisecond, DefaultMemoryKB: int64(row.DefaultMemoryLimitKb), Enabled: row.Enabled}
}

func text(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func textPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func boolPtr(value *bool) pgtype.Bool {
	if value == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *value, Valid: true}
}

func int4(value int) pgtype.Int4 {
	if value <= 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(value), Valid: true}
}

func int4Ptr(value *int32) pgtype.Int4 {
	if value == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *value, Valid: true}
}

func int32Pg(value *int32) pgtype.Int4 {
	if value == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *value, Valid: true}
}

func validInt8(value int64) pgtype.Int8 {
	return pgtype.Int8{Int64: value, Valid: value > 0}
}

func int8Ptr(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *value, Valid: true}
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func judgedAtParam(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return timestamptz(value)
}

func int4Value(value pgtype.Int4) *int32 {
	if !value.Valid {
		return nil
	}
	out := value.Int32
	return &out
}

func int8Value(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	out := value.Int64
	return &out
}

func textValue(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	out := value.String
	return &out
}

func timeValue(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	out := value.Time
	return &out
}

func int64String(value int64) string {
	return strconv.FormatInt(value, 10)
}

func mapNotFound(err error, code, message string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound(code, message)
	}
	return err
}
