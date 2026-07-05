package submission

import (
	"context"
	"encoding/json"
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
	CompleteSubmissionWithResult(ctx context.Context, id int64, result judge.Result, score int32) (SubmissionRecord, error)
	GetLatestJudgeAttemptBySubmissionID(ctx context.Context, submissionID int64) (JudgeAttemptRecord, error)
	ListJudgeCaseResults(ctx context.Context, attemptID int64) ([]JudgeCaseResultRecord, error)
	GetSubmissionResult(ctx context.Context, submissionID int64) (SubmissionResultRecord, error)
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

func (r *SQLRepository) CompleteSubmissionWithResult(ctx context.Context, id int64, result judge.Result, score int32) (SubmissionRecord, error) {
	if r.txRunner == nil {
		return SubmissionRecord{}, errors.New("transaction runner is required to complete submission with judge result")
	}
	params := db.UpdateSubmissionStatusParams{
		Status:       dbStatus(result.Verdict),
		TimeMs:       int4(result.TimeMS),
		MemoryKb:     int4(result.MemoryKB),
		Score:        pgtype.Int4{Int32: score, Valid: true},
		ErrorMessage: text(result.ErrorMessage),
		ID:           id,
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
		attempt, err := persistJudgeResult(ctx, q, record, result, score)
		if err != nil {
			return err
		}
		return updateContestProblemResult(ctx, q, record, attempt.ID)
	})
	return record, err
}

func persistJudgeResult(ctx context.Context, q *db.Queries, submission SubmissionRecord, result judge.Result, score int32) (db.JudgeAttempt, error) {
	summary := judgeSummary(result)
	manifest, err := judgeManifestJSON(result.Manifest)
	if err != nil {
		return db.JudgeAttempt{}, err
	}
	safeSummary, err := json.Marshal(summary)
	if err != nil {
		return db.JudgeAttempt{}, err
	}
	metrics, err := json.Marshal(map[string]any{})
	if err != nil {
		return db.JudgeAttempt{}, err
	}

	finishedAt := result.JudgedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	latest, err := q.GetLatestJudgeAttemptBySubmissionID(ctx, pgtype.Int8{Int64: submission.ID, Valid: true})
	attemptNo := int32(1)
	if err == nil {
		attemptNo = latest.AttemptNo + 1
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.JudgeAttempt{}, err
	}

	attempt, err := q.CreateJudgeAttempt(ctx, db.CreateJudgeAttemptParams{
		SubmissionID:         pgtype.Int8{Int64: submission.ID, Valid: true},
		AttemptNo:            attemptNo,
		ProtocolVersion:      judge.ProtocolVersion,
		JudgeCoreVersion:     defaultJudgeCoreVersion(result.Manifest),
		JudgeEngine:          judge.EngineSOJAgent,
		JudgeAgentID:         text(result.Manifest.JudgeAgentID),
		LanguageID:           submission.LanguageID,
		LanguageRuntime:      text(result.Manifest.LanguageRuntime),
		SandboxBackend:       text(result.Manifest.SandboxBackend),
		SandboxProfile:       text(result.Manifest.SandboxProfile),
		TestcaseSetID:        pgtype.Int8{Int64: submission.TestcaseSetID, Valid: submission.TestcaseSetID > 0},
		TestcaseSetHash:      text(result.Manifest.TestcaseSetHash),
		CheckerHash:          text(result.Manifest.CheckerHash),
		ValidatorHash:        text(result.Manifest.ValidatorHash),
		Status:               dbStatus(result.Verdict),
		Verdict:              text(string(result.Verdict)),
		Score:                score,
		TimeMs:               int4(result.TimeMS),
		MemoryKb:             int4(result.MemoryKB),
		FirstFailedCaseIndex: summary.firstFailedCaseIndex(),
		FirstFailedGroup:     text(summary.FirstFailedGroup),
		CompileOutputSummary: text(summary.CompileOutputSummary),
		StderrSummary:        text(summary.StderrSummary),
		CheckerMessage:       text(summary.CheckerMessage),
		ErrorClass:           text(summary.ErrorClass),
		ErrorMessage:         text(result.ErrorMessage),
		Manifest:             manifest,
		Metrics:              metrics,
		TraceID:              text(result.Manifest.TraceID),
		StartedAt:            timestamptz(finishedAt),
		FinishedAt:           timestamptz(finishedAt),
	})
	if err != nil {
		return db.JudgeAttempt{}, err
	}

	for _, item := range result.Cases {
		_, err := q.CreateJudgeCaseResult(ctx, db.CreateJudgeCaseResultParams{
			AttemptID:         attempt.ID,
			CaseIndex:         int32(item.Index),
			GroupName:         text(item.GroupName),
			TestcaseKey:       text(item.TestcaseKey),
			Status:            dbStatus(item.Verdict),
			Score:             item.Score,
			TimeMs:            int4(item.TimeMS),
			MemoryKb:          int4(item.MemoryKB),
			ExitCode:          int32Pg(item.ExitCode),
			Signal:            text(item.Signal),
			CheckerMessage:    text(item.CheckerMessage),
			OutputDiffSummary: text(item.OutputDiffSummary),
		})
		if err != nil {
			return db.JudgeAttempt{}, err
		}
	}

	_, err = q.UpsertSubmissionResult(ctx, db.UpsertSubmissionResultParams{
		SubmissionID:         submission.ID,
		AttemptID:            attempt.ID,
		Status:               submission.Status,
		Score:                score,
		TimeMs:               int4(result.TimeMS),
		MemoryKb:             int4(result.MemoryKB),
		FirstFailedCaseIndex: summary.firstFailedCaseIndex(),
		FirstFailedGroup:     text(summary.FirstFailedGroup),
		ErrorClass:           text(summary.ErrorClass),
		SafeSummary:          safeSummary,
	})
	if err != nil {
		return db.JudgeAttempt{}, err
	}
	return attempt, nil
}

type safeJudgeSummary struct {
	Verdict              string `json:"verdict"`
	TimeMS               int    `json:"time_ms,omitempty"`
	MemoryKB             int    `json:"memory_kb,omitempty"`
	CompileOutputSummary string `json:"compile_output_summary,omitempty"`
	StderrSummary        string `json:"stderr_summary,omitempty"`
	FirstFailedCaseIndex *int32 `json:"first_failed_case_index,omitempty"`
	FirstFailedGroup     string `json:"first_failed_group,omitempty"`
	CheckerMessage       string `json:"checker_message,omitempty"`
	ErrorClass           string `json:"error_class,omitempty"`
	ErrorMessage         string `json:"error_message,omitempty"`
}

func (s safeJudgeSummary) firstFailedCaseIndex() pgtype.Int4 {
	if s.FirstFailedCaseIndex == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *s.FirstFailedCaseIndex, Valid: true}
}

func judgeSummary(result judge.Result) safeJudgeSummary {
	summary := safeJudgeSummary{
		Verdict:              string(result.Verdict),
		TimeMS:               result.TimeMS,
		MemoryKB:             result.MemoryKB,
		CompileOutputSummary: truncateSummary(result.CompileOutput),
		StderrSummary:        truncateSummary(result.Stderr),
		ErrorMessage:         result.ErrorMessage,
	}
	if result.Verdict == judge.VerdictSystemError {
		summary.ErrorClass = "system_error"
	}
	if result.Verdict == judge.VerdictCompileError {
		summary.ErrorClass = "compile_error"
	}
	for _, item := range result.Cases {
		if item.Verdict == judge.VerdictAccepted {
			continue
		}
		index := int32(item.Index)
		summary.FirstFailedCaseIndex = &index
		summary.FirstFailedGroup = item.GroupName
		summary.CheckerMessage = item.CheckerMessage
		if summary.ErrorMessage == "" {
			summary.ErrorMessage = item.OutputDiffSummary
		}
		break
	}
	if summary.CheckerMessage == "" {
		for _, item := range result.Cases {
			if item.CheckerMessage != "" {
				summary.CheckerMessage = item.CheckerMessage
				break
			}
		}
	}
	return summary
}

func judgeManifestJSON(manifest judge.Manifest) ([]byte, error) {
	raw := make(map[string]any, len(manifest.Raw)+9)
	for key, value := range manifest.Raw {
		raw[key] = value
	}
	setIfNotEmpty(raw, "judge_core_version", defaultJudgeCoreVersion(manifest))
	setIfNotEmpty(raw, "judge_agent_id", manifest.JudgeAgentID)
	setIfNotEmpty(raw, "language_runtime", manifest.LanguageRuntime)
	setIfNotEmpty(raw, "sandbox_backend", manifest.SandboxBackend)
	setIfNotEmpty(raw, "sandbox_profile", manifest.SandboxProfile)
	setIfNotEmpty(raw, "testcase_set_hash", manifest.TestcaseSetHash)
	setIfNotEmpty(raw, "checker_hash", manifest.CheckerHash)
	setIfNotEmpty(raw, "validator_hash", manifest.ValidatorHash)
	setIfNotEmpty(raw, "trace_id", manifest.TraceID)
	return json.Marshal(raw)
}

func setIfNotEmpty(values map[string]any, key string, value string) {
	if value != "" {
		values[key] = value
	}
}

func defaultJudgeCoreVersion(manifest judge.Manifest) string {
	if manifest.JudgeCoreVersion != "" {
		return manifest.JudgeCoreVersion
	}
	return judge.ProtocolVersion
}

func truncateSummary(value string) string {
	const limit = 4096
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func updateContestProblemResult(ctx context.Context, q *db.Queries, submission SubmissionRecord, attemptID int64) error {
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
	bestSubmissionID := current.BestSubmissionID
	bestAttemptID := current.BestAttemptID
	if submission.Status == StatusAccepted {
		bestSubmissionID = pgtype.Int8{Int64: submission.ID, Valid: true}
		bestAttemptID = pgtype.Int8{Int64: attemptID, Valid: true}
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
		BestSubmissionID: bestSubmissionID,
		BestAttemptID:    bestAttemptID,
		LastAttemptID:    pgtype.Int8{Int64: attemptID, Valid: true},
	})
	return err
}

func (r *SQLRepository) GetLatestJudgeAttemptBySubmissionID(ctx context.Context, submissionID int64) (JudgeAttemptRecord, error) {
	row, err := r.q.GetLatestJudgeAttemptBySubmissionID(ctx, pgtype.Int8{Int64: submissionID, Valid: true})
	return judgeAttemptRecord(row), mapNotFound(err, "judge_attempt.not_found", "judge attempt not found")
}

func (r *SQLRepository) ListJudgeCaseResults(ctx context.Context, attemptID int64) ([]JudgeCaseResultRecord, error) {
	rows, err := r.q.ListJudgeCaseResultsByAttemptID(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	out := make([]JudgeCaseResultRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, judgeCaseResultRecord(row))
	}
	return out, nil
}

func (r *SQLRepository) GetSubmissionResult(ctx context.Context, submissionID int64) (SubmissionResultRecord, error) {
	row, err := r.q.GetSubmissionResultBySubmissionID(ctx, submissionID)
	return submissionResultRecord(row), mapNotFound(err, "submission_result.not_found", "submission result not found")
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

func judgeAttemptRecord(row db.JudgeAttempt) JudgeAttemptRecord {
	return JudgeAttemptRecord{
		ID:                   row.ID,
		SubmissionID:         int8Value(row.SubmissionID),
		RunID:                int8Value(row.RunID),
		TaskID:               int8Value(row.TaskID),
		RejudgeBatchID:       int8Value(row.RejudgeBatchID),
		AttemptNo:            row.AttemptNo,
		ProtocolVersion:      row.ProtocolVersion,
		JudgeCoreVersion:     row.JudgeCoreVersion,
		JudgeEngine:          row.JudgeEngine,
		JudgeAgentID:         textValue(row.JudgeAgentID),
		LanguageID:           row.LanguageID,
		LanguageRuntime:      textValue(row.LanguageRuntime),
		SandboxBackend:       textValue(row.SandboxBackend),
		SandboxProfile:       textValue(row.SandboxProfile),
		TestcaseSetID:        int8Value(row.TestcaseSetID),
		TestcaseSetHash:      textValue(row.TestcaseSetHash),
		CheckerHash:          textValue(row.CheckerHash),
		ValidatorHash:        textValue(row.ValidatorHash),
		Status:               row.Status,
		Verdict:              textValue(row.Verdict),
		Score:                row.Score,
		TimeMS:               int4Value(row.TimeMs),
		MemoryKB:             int4Value(row.MemoryKb),
		FirstFailedCaseIndex: int4Value(row.FirstFailedCaseIndex),
		FirstFailedGroup:     textValue(row.FirstFailedGroup),
		CompileOutputSummary: textValue(row.CompileOutputSummary),
		StderrSummary:        textValue(row.StderrSummary),
		CheckerMessage:       textValue(row.CheckerMessage),
		ErrorClass:           textValue(row.ErrorClass),
		ErrorMessage:         textValue(row.ErrorMessage),
		Manifest:             append([]byte(nil), row.Manifest...),
		Metrics:              append([]byte(nil), row.Metrics...),
		TraceID:              textValue(row.TraceID),
		StartedAt:            timeValue(row.StartedAt),
		FinishedAt:           timeValue(row.FinishedAt),
		CreatedAt:            row.CreatedAt.Time,
		UpdatedAt:            row.UpdatedAt.Time,
	}
}

func judgeCaseResultRecord(row db.JudgeCaseResult) JudgeCaseResultRecord {
	return JudgeCaseResultRecord{
		ID:                row.ID,
		AttemptID:         row.AttemptID,
		CaseIndex:         row.CaseIndex,
		GroupName:         textValue(row.GroupName),
		TestcaseKey:       textValue(row.TestcaseKey),
		Status:            row.Status,
		Score:             row.Score,
		TimeMS:            int4Value(row.TimeMs),
		MemoryKB:          int4Value(row.MemoryKb),
		ExitCode:          int4Value(row.ExitCode),
		Signal:            textValue(row.Signal),
		CheckerMessage:    textValue(row.CheckerMessage),
		OutputDiffSummary: textValue(row.OutputDiffSummary),
		StdoutArtifactID:  int8Value(row.StdoutArtifactID),
		StderrArtifactID:  int8Value(row.StderrArtifactID),
		DiffArtifactID:    int8Value(row.DiffArtifactID),
		CreatedAt:         row.CreatedAt.Time,
	}
}

func submissionResultRecord(row db.SubmissionResult) SubmissionResultRecord {
	return SubmissionResultRecord{
		SubmissionID:         row.SubmissionID,
		AttemptID:            row.AttemptID,
		Status:               row.Status,
		Score:                row.Score,
		TimeMS:               int4Value(row.TimeMs),
		MemoryKB:             int4Value(row.MemoryKb),
		FirstFailedCaseIndex: int4Value(row.FirstFailedCaseIndex),
		FirstFailedGroup:     textValue(row.FirstFailedGroup),
		ErrorClass:           textValue(row.ErrorClass),
		SafeSummary:          append([]byte(nil), row.SafeSummary...),
		CreatedAt:            row.CreatedAt.Time,
		UpdatedAt:            row.UpdatedAt.Time,
	}
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
