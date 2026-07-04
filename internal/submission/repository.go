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
	UpdatedAt        time.Time
}

type RunRecord struct {
	ID               int64
	UserID           int64
	ProblemID        int64
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
	ID           int64
	SubmissionID int64
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

type ListSubmissionsInput struct {
	UserID    *int64
	ProblemID *int64
	ContestID *int64
	Status    *string
	Offset    int32
	Limit     int32
}

type Repository interface {
	CreateArtifact(ctx context.Context, arg ArtifactRecord) (ArtifactRecord, error)
	GetArtifact(ctx context.Context, id int64) (ArtifactRecord, error)
	CreateSubmission(ctx context.Context, arg SubmissionRecord) (SubmissionRecord, error)
	CreateSubmissionWithTask(ctx context.Context, arg SubmissionRecord, nextRunAt time.Time) (SubmissionRecord, JudgeTaskRecord, error)
	GetSubmission(ctx context.Context, id int64) (SubmissionRecord, error)
	ListSubmissions(ctx context.Context, input ListSubmissionsInput) ([]SubmissionRecord, int64, error)
	MarkSubmissionRunning(ctx context.Context, id int64) (SubmissionRecord, error)
	MarkSubmissionQueued(ctx context.Context, id int64, reason string) (SubmissionRecord, error)
	MarkSubmissionSystemError(ctx context.Context, id int64, reason string) (SubmissionRecord, error)
	UpdateSubmissionStatus(ctx context.Context, id int64, result judge.Result, score int32) (SubmissionRecord, error)
	CreateJudgeTask(ctx context.Context, submissionID int64, nextRunAt time.Time) (JudgeTaskRecord, error)
	GetJudgeTask(ctx context.Context, id int64) (JudgeTaskRecord, error)
	ClaimPendingJudgeTasks(ctx context.Context, limit int32) ([]JudgeTaskRecord, error)
	MarkJudgeTaskDispatching(ctx context.Context, id int64) (JudgeTaskRecord, error)
	MarkJudgeTaskDispatched(ctx context.Context, id int64, streamID string) (JudgeTaskRecord, error)
	MarkJudgeTaskRunning(ctx context.Context, id int64) (JudgeTaskRecord, error)
	MarkJudgeTaskDone(ctx context.Context, id int64) (JudgeTaskRecord, error)
	RetryJudgeTask(ctx context.Context, id int64, nextRunAt time.Time, reason string) (JudgeTaskRecord, error)
	MarkJudgeTaskDead(ctx context.Context, id int64, reason string) (JudgeTaskRecord, error)
	CreateRun(ctx context.Context, arg RunRecord) (RunRecord, error)
	GetRun(ctx context.Context, id int64) (RunRecord, error)
	UpdateRunStatus(ctx context.Context, id int64, result judge.Result) (RunRecord, error)
	ResetStaleJudgeTasks(ctx context.Context, staleBefore time.Time, reason string) ([]JudgeTaskRecord, error)
	MarkStaleRunsSystemError(ctx context.Context, staleBefore time.Time, reason string) ([]RunRecord, error)
	GetEnabledLanguage(ctx context.Context, id int64) (LanguageRecord, error)
	ListLanguages(ctx context.Context, arg ListLanguagesInput) ([]LanguageRecord, int64, error)
	UpsertLanguage(ctx context.Context, language judge.Language) (LanguageRecord, error)
	UpdateLanguage(ctx context.Context, id int64, arg UpdateLanguageInput) (LanguageRecord, error)
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

func (r *SQLRepository) CreateSubmission(ctx context.Context, arg SubmissionRecord) (SubmissionRecord, error) {
	row, err := createSubmissionRow(ctx, r.q, arg)
	return submissionRecord(row), err
}

func (r *SQLRepository) CreateSubmissionWithTask(ctx context.Context, arg SubmissionRecord, nextRunAt time.Time) (SubmissionRecord, JudgeTaskRecord, error) {
	if r.txRunner == nil {
		return SubmissionRecord{}, JudgeTaskRecord{}, errors.New("transaction runner is required to create submission with judge task")
	}

	var submission SubmissionRecord
	var task JudgeTaskRecord
	err := postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		submissionRow, err := createSubmissionRow(ctx, q, arg)
		if err != nil {
			return err
		}
		submission = submissionRecord(submissionRow)
		taskRow, err := q.CreateJudgeTask(ctx, db.CreateJudgeTaskParams{
			SubmissionID: submission.ID,
			Status:       "pending",
			NextRunAt:    timestamptz(nextRunAt),
		})
		if err != nil {
			return err
		}
		task = judgeTaskRecord(taskRow)
		return nil
	})
	if err != nil {
		return SubmissionRecord{}, JudgeTaskRecord{}, err
	}
	return submission, task, nil
}

func createSubmissionRow(ctx context.Context, q *db.Queries, arg SubmissionRecord) (db.Submission, error) {
	return q.CreateSubmission(ctx, db.CreateSubmissionParams{
		UserID:           arg.UserID,
		ProblemID:        arg.ProblemID,
		ContestID:        int8Ptr(arg.ContestID),
		LanguageID:       arg.LanguageID,
		TestcaseSetID:    arg.TestcaseSetID,
		Status:           arg.Status,
		SourceArtifactID: validInt8(arg.SourceArtifactID),
	})
}

func (r *SQLRepository) GetSubmission(ctx context.Context, id int64) (SubmissionRecord, error) {
	row, err := r.q.GetSubmissionByID(ctx, id)
	return submissionRecord(row), mapNotFound(err, "submission.not_found", "submission not found")
}

func (r *SQLRepository) ListSubmissions(ctx context.Context, input ListSubmissionsInput) ([]SubmissionRecord, int64, error) {
	params := db.ListSubmissionsParams{
		UserID:    int8Ptr(input.UserID),
		ProblemID: int8Ptr(input.ProblemID),
		ContestID: int8Ptr(input.ContestID),
		Status:    textPtr(input.Status),
		Offset:    input.Offset,
		Limit:     input.Limit,
	}
	rows, err := r.q.ListSubmissions(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	total, err := r.q.CountSubmissions(ctx, db.CountSubmissionsParams{
		UserID:    params.UserID,
		ProblemID: params.ProblemID,
		ContestID: params.ContestID,
		Status:    params.Status,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]SubmissionRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, submissionRecord(row))
	}
	return out, total, nil
}

func (r *SQLRepository) MarkSubmissionRunning(ctx context.Context, id int64) (SubmissionRecord, error) {
	row, err := r.q.MarkSubmissionRunning(ctx, id)
	return submissionRecord(row), err
}

func (r *SQLRepository) MarkSubmissionQueued(ctx context.Context, id int64, reason string) (SubmissionRecord, error) {
	row, err := r.q.MarkSubmissionQueued(ctx, db.MarkSubmissionQueuedParams{ID: id, ErrorMessage: text(reason)})
	if errors.Is(err, pgx.ErrNoRows) {
		return r.GetSubmission(ctx, id)
	}
	return submissionRecord(row), err
}

func (r *SQLRepository) MarkSubmissionSystemError(ctx context.Context, id int64, reason string) (SubmissionRecord, error) {
	row, err := r.q.MarkSubmissionSystemError(ctx, db.MarkSubmissionSystemErrorParams{ID: id, ErrorMessage: text(reason)})
	if errors.Is(err, pgx.ErrNoRows) {
		return r.GetSubmission(ctx, id)
	}
	return submissionRecord(row), err
}

func (r *SQLRepository) UpdateSubmissionStatus(ctx context.Context, id int64, result judge.Result, score int32) (SubmissionRecord, error) {
	params := db.UpdateSubmissionStatusParams{
		Status:       dbStatus(result.Verdict),
		TimeMs:       int4(result.TimeMS),
		MemoryKb:     int4(result.MemoryKB),
		Score:        pgtype.Int4{Int32: score, Valid: true},
		ErrorMessage: text(result.ErrorMessage),
		ID:           id,
	}
	if r.txRunner == nil {
		row, err := r.q.UpdateSubmissionStatus(ctx, params)
		if errors.Is(err, pgx.ErrNoRows) {
			return r.GetSubmission(ctx, id)
		}
		record := submissionRecord(row)
		if err != nil {
			return record, err
		}
		return record, updateContestProblemResult(ctx, r.q, record)
	}

	var record SubmissionRecord
	err := postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.UpdateSubmissionStatus(ctx, params)
		if errors.Is(err, pgx.ErrNoRows) {
			row, err = q.GetSubmissionByID(ctx, id)
			record = submissionRecord(row)
			return err
		}
		record = submissionRecord(row)
		if err != nil {
			return err
		}
		return updateContestProblemResult(ctx, q, record)
	})
	return record, err
}

func updateContestProblemResult(ctx context.Context, q *db.Queries, submission SubmissionRecord) error {
	if submission.ContestID == nil {
		return nil
	}
	contest, err := q.GetContestByID(ctx, *submission.ContestID)
	if err != nil {
		return err
	}
	problems, err := q.ListContestProblems(ctx, *submission.ContestID)
	if err != nil {
		return err
	}
	inContest := false
	for _, problem := range problems {
		if problem.ProblemID == submission.ProblemID {
			inContest = true
			break
		}
	}
	if !inContest {
		return nil
	}

	current := db.ContestProblemResult{
		ContestID: *submission.ContestID,
		UserID:    submission.UserID,
		ProblemID: submission.ProblemID,
		Status:    "none",
	}
	results, err := q.ListContestProblemResults(ctx, *submission.ContestID)
	if err != nil {
		return err
	}
	for _, result := range results {
		if result.UserID == submission.UserID && result.ProblemID == submission.ProblemID {
			current = result
			break
		}
	}
	if current.LastSubmissionID.Valid && current.LastSubmissionID.Int64 == submission.ID {
		return nil
	}
	if current.Status == StatusAccepted {
		return nil
	}

	attempts := current.Attempts + 1
	status := "attempted"
	acceptedAt := pgtype.Timestamptz{}
	penaltyMinutes := current.PenaltyMinutes
	if submission.Status == StatusAccepted {
		status = "accepted"
		accepted := submission.SubmittedAt
		acceptedAt = timestamptz(accepted)
		penaltyMinutes = int32(accepted.Sub(contest.StartAt.Time).Minutes()) + (attempts-1)*20
	}
	_, err = q.UpsertContestProblemResult(ctx, db.UpsertContestProblemResultParams{
		ContestID:        *submission.ContestID,
		UserID:           submission.UserID,
		ProblemID:        submission.ProblemID,
		Status:           status,
		Attempts:         attempts,
		AcceptedAt:       acceptedAt,
		PenaltyMinutes:   penaltyMinutes,
		LastSubmissionID: pgtype.Int8{Int64: submission.ID, Valid: true},
	})
	return err
}

func (r *SQLRepository) CreateJudgeTask(ctx context.Context, submissionID int64, nextRunAt time.Time) (JudgeTaskRecord, error) {
	row, err := r.q.CreateJudgeTask(ctx, db.CreateJudgeTaskParams{
		SubmissionID: submissionID,
		Status:       "pending",
		NextRunAt:    timestamptz(nextRunAt),
	})
	return judgeTaskRecord(row), err
}

func (r *SQLRepository) GetJudgeTask(ctx context.Context, id int64) (JudgeTaskRecord, error) {
	row, err := r.q.GetJudgeTaskByID(ctx, id)
	return judgeTaskRecord(row), mapNotFound(err, "judge_task.not_found", "judge task not found")
}

func (r *SQLRepository) ClaimPendingJudgeTasks(ctx context.Context, limit int32) ([]JudgeTaskRecord, error) {
	rows, err := r.q.ClaimPendingJudgeTasks(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]JudgeTaskRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, judgeTaskRecord(row))
	}
	return out, nil
}

func (r *SQLRepository) MarkJudgeTaskDispatching(ctx context.Context, id int64) (JudgeTaskRecord, error) {
	row, err := r.q.UpdateJudgeTaskDispatching(ctx, id)
	return judgeTaskRecord(row), err
}

func (r *SQLRepository) MarkJudgeTaskDispatched(ctx context.Context, id int64, streamID string) (JudgeTaskRecord, error) {
	row, err := r.q.MarkJudgeTaskDispatched(ctx, db.MarkJudgeTaskDispatchedParams{ID: id, StreamID: text(streamID)})
	return judgeTaskRecord(row), err
}

func (r *SQLRepository) MarkJudgeTaskRunning(ctx context.Context, id int64) (JudgeTaskRecord, error) {
	row, err := r.q.MarkJudgeTaskRunning(ctx, id)
	return judgeTaskRecord(row), err
}

func (r *SQLRepository) MarkJudgeTaskDone(ctx context.Context, id int64) (JudgeTaskRecord, error) {
	row, err := r.q.MarkJudgeTaskDone(ctx, id)
	return judgeTaskRecord(row), err
}

func (r *SQLRepository) RetryJudgeTask(ctx context.Context, id int64, nextRunAt time.Time, reason string) (JudgeTaskRecord, error) {
	row, err := r.q.RetryJudgeTask(ctx, db.RetryJudgeTaskParams{ID: id, NextRunAt: timestamptz(nextRunAt), LastError: text(reason)})
	return judgeTaskRecord(row), err
}

func (r *SQLRepository) MarkJudgeTaskDead(ctx context.Context, id int64, reason string) (JudgeTaskRecord, error) {
	row, err := r.q.MarkJudgeTaskDead(ctx, db.MarkJudgeTaskDeadParams{ID: id, LastError: text(reason)})
	return judgeTaskRecord(row), err
}

func (r *SQLRepository) CreateRun(ctx context.Context, arg RunRecord) (RunRecord, error) {
	row, err := r.q.CreateRun(ctx, db.CreateRunParams{
		UserID:           arg.UserID,
		ProblemID:        arg.ProblemID,
		LanguageID:       arg.LanguageID,
		Status:           arg.Status,
		SourceArtifactID: validInt8(arg.SourceArtifactID),
		Stdin:            text(arg.Stdin),
	})
	return runRecord(row), err
}

func (r *SQLRepository) GetRun(ctx context.Context, id int64) (RunRecord, error) {
	row, err := r.q.GetRunByID(ctx, id)
	return runRecord(row), mapNotFound(err, "run.not_found", "run not found")
}

func (r *SQLRepository) UpdateRunStatus(ctx context.Context, id int64, result judge.Result) (RunRecord, error) {
	row, err := r.q.UpdateRunStatus(ctx, db.UpdateRunStatusParams{
		Status:        dbStatus(result.Verdict),
		Stdout:        text(result.Stdout),
		Stderr:        text(result.Stderr),
		CompileOutput: text(result.CompileOutput),
		TimeMs:        int4(result.TimeMS),
		MemoryKb:      int4(result.MemoryKB),
		ErrorMessage:  text(result.ErrorMessage),
		ID:            id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return r.GetRun(ctx, id)
	}
	return runRecord(row), err
}

func (r *SQLRepository) ResetStaleJudgeTasks(ctx context.Context, staleBefore time.Time, reason string) ([]JudgeTaskRecord, error) {
	rows, err := r.q.ResetStaleJudgeTasks(ctx, db.ResetStaleJudgeTasksParams{StaleBefore: timestamptz(staleBefore), LastError: text(reason)})
	if err != nil {
		return nil, err
	}
	out := make([]JudgeTaskRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, resetJudgeTaskRecord(row))
	}
	return out, nil
}

func (r *SQLRepository) MarkStaleRunsSystemError(ctx context.Context, staleBefore time.Time, reason string) ([]RunRecord, error) {
	rows, err := r.q.MarkStaleRunsSystemError(ctx, db.MarkStaleRunsSystemErrorParams{StaleBefore: timestamptz(staleBefore), ErrorMessage: text(reason)})
	if err != nil {
		return nil, err
	}
	out := make([]RunRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, runRecord(row))
	}
	return out, nil
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
		Engine:               "judge0",
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
	return ArtifactRecord{ID: row.ID, OwnerType: row.OwnerType, OwnerID: row.OwnerID, Kind: row.Kind, StorageKey: row.StorageKey, ChecksumSHA256: row.ChecksumSha256, SizeBytes: row.SizeBytes, ContentType: row.ContentType}
}

func submissionRecord(row db.Submission) SubmissionRecord {
	return SubmissionRecord{ID: row.ID, UserID: row.UserID, ProblemID: row.ProblemID, ContestID: int8Value(row.ContestID), LanguageID: row.LanguageID, TestcaseSetID: row.TestcaseSetID, Status: row.Status, SourceArtifactID: row.SourceArtifactID.Int64, TimeMS: int4Value(row.TimeMs), MemoryKB: int4Value(row.MemoryKb), Score: row.Score, ErrorMessage: textValue(row.ErrorMessage), SubmittedAt: row.SubmittedAt.Time, JudgedAt: timeValue(row.JudgedAt), UpdatedAt: row.UpdatedAt.Time}
}

func runRecord(row db.Run) RunRecord {
	return RunRecord{ID: row.ID, UserID: row.UserID, ProblemID: row.ProblemID, LanguageID: row.LanguageID, Status: row.Status, SourceArtifactID: row.SourceArtifactID.Int64, Stdin: row.Stdin.String, Stdout: row.Stdout.String, Stderr: row.Stderr.String, CompileOutput: row.CompileOutput.String, TimeMS: int4Value(row.TimeMs), MemoryKB: int4Value(row.MemoryKb), ErrorMessage: textValue(row.ErrorMessage), CreatedAt: row.CreatedAt.Time, FinishedAt: timeValue(row.FinishedAt), UpdatedAt: row.UpdatedAt.Time}
}

func judgeTaskRecord(row db.JudgeTask) JudgeTaskRecord {
	return JudgeTaskRecord{ID: row.ID, SubmissionID: row.SubmissionID, StreamID: row.StreamID.String, Status: row.Status, Attempts: row.Attempts, NextRunAt: row.NextRunAt.Time, LastError: row.LastError.String}
}

func resetJudgeTaskRecord(row db.ResetStaleJudgeTasksRow) JudgeTaskRecord {
	return JudgeTaskRecord{ID: row.ID, SubmissionID: row.SubmissionID, StreamID: row.StreamID.String, Status: row.Status, Attempts: row.Attempts, NextRunAt: row.NextRunAt.Time, LastError: row.LastError.String}
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
