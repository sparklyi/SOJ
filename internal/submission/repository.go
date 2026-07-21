package submission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/judge"
	"SOJ/internal/postgres"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

type Repository interface {
	CreateArtifact(ctx context.Context, arg ArtifactRecord) (ArtifactRecord, error)
	GetArtifact(ctx context.Context, id int64) (ArtifactRecord, error)
	CreateSubmission(ctx context.Context, arg SubmissionRecord) (SubmissionRecord, error)
	CreateSubmissionWithTask(ctx context.Context, arg SubmissionRecord, nextRunAt time.Time) (SubmissionRecord, JudgeTaskRecord, error)
	GetSubmission(ctx context.Context, id int64) (SubmissionRecord, error)
	ListSubmissions(ctx context.Context, input ListSubmissionsInput) ([]SubmissionRecord, int64, error)
	ListSubmissionsByCursor(ctx context.Context, input ListSubmissionsInput) ([]SubmissionRecord, error)
	ListSubmissionsByUserBefore(ctx context.Context, userID int64, cursor SubmissionCursor, limit int32) ([]SubmissionRecord, error)
	ListSubmissionSummaries(ctx context.Context, submissionIDs []int64, includeAttempts bool) (map[int64]SubmissionListSummary, error)
	MarkSubmissionRunning(ctx context.Context, id int64) (SubmissionRecord, error)
	MarkSubmissionQueued(ctx context.Context, id int64, reason string) (SubmissionRecord, error)
	MarkSubmissionSystemError(ctx context.Context, id int64, reason string) (SubmissionRecord, error)
	CompleteSubmissionWithResult(ctx context.Context, id int64, result judge.Result, score int32) (SubmissionRecord, error)
	EnsureJudgeAttempt(ctx context.Context, input EnsureJudgeAttemptInput) (JudgeAttemptRecord, error)
	CompleteJudgeAttemptResult(ctx context.Context, input CompleteJudgeAttemptResultInput) (SubmissionRecord, bool, error)
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
	RecoverDeadJudgeTask(ctx context.Context, id int64, nextRunAt time.Time, reason string) (JudgeTaskRecord, error)
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

func (r *SQLRepository) CreateRejudgeBatchWithItems(ctx context.Context, input CreateRejudgeBatchRecordInput) (RejudgeBatchRecord, error) {
	if r.txRunner == nil {
		return RejudgeBatchRecord{}, errors.New("transaction runner is required to create rejudge batch")
	}
	var batch RejudgeBatchRecord
	err := postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		var submissions []db.Submission
		var err error
		switch {
		case input.ProblemID != nil:
			submissions, err = q.ListEligibleProblemSubmissionsForRejudge(ctx, *input.ProblemID)
		case input.ContestID != nil:
			submissions, err = q.ListEligibleContestSubmissionsForRejudge(ctx, pgtype.Int8{Int64: *input.ContestID, Valid: true})
		default:
			return errors.New("rejudge target is required")
		}
		if err != nil {
			return err
		}
		if len(submissions) == 0 {
			return ErrNoRejudgeSubmissions
		}
		row, err := q.CreateRejudgeBatch(ctx, db.CreateRejudgeBatchParams{
			ProblemID: int8Ptr(input.ProblemID), ContestID: int8Ptr(input.ContestID), RequestedBy: input.RequestedBy,
			Status: RejudgeBatchStatusQueued, Reason: input.Reason, Filters: []byte(`{}`), TotalCount: int32(len(submissions)),
		})
		if err != nil {
			return err
		}
		batch = rejudgeBatchRecord(row)
		for _, submission := range submissions {
			task, err := q.GetJudgeTaskBySubmissionID(ctx, submission.ID)
			if err != nil {
				return err
			}
			if _, err := q.CreateRejudgeBatchItem(ctx, db.CreateRejudgeBatchItemParams{BatchID: batch.ID, SubmissionID: submission.ID, TaskID: task.ID}); err != nil {
				return err
			}
			if _, err := q.PrepareJudgeTaskForRejudge(ctx, db.PrepareJudgeTaskForRejudgeParams{NextRunAt: timestamptz(input.NextRunAt), ID: task.ID, SubmissionID: submission.ID}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return apperror.Conflict("rejudge.task_not_ready", "submission judge task is not done or dead")
				}
				return err
			}
			if _, err := q.PrepareSubmissionForRejudge(ctx, submission.ID); err != nil {
				return err
			}
		}
		return nil
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return RejudgeBatchRecord{}, apperror.Conflict("rejudge.active_conflict", "a submission already belongs to an active rejudge batch")
	}
	return batch, err
}

func (r *SQLRepository) GetRejudgeBatch(ctx context.Context, id int64) (RejudgeBatchRecord, error) {
	row, err := r.q.GetRejudgeBatchByID(ctx, id)
	return rejudgeBatchRecord(row), mapNotFound(err, "rejudge.not_found", "rejudge batch not found")
}

func (r *SQLRepository) ListRejudgeBatches(ctx context.Context, input ListRejudgeBatchesInput) ([]RejudgeBatchRecord, int64, error) {
	params := db.ListRejudgeBatchesParams{
		ProblemID: int8Ptr(input.ProblemID), ContestID: int8Ptr(input.ContestID), RequestedBy: int8Ptr(input.RequestedBy),
		Status: textPtr(input.Status), Offset: input.Offset, Limit: input.Limit,
	}
	rows, err := r.q.ListRejudgeBatches(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	total, err := r.q.CountRejudgeBatches(ctx, db.CountRejudgeBatchesParams{ProblemID: params.ProblemID, ContestID: params.ContestID, RequestedBy: params.RequestedBy, Status: params.Status})
	if err != nil {
		return nil, 0, err
	}
	out := make([]RejudgeBatchRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, rejudgeBatchRecord(row))
	}
	return out, total, nil
}

func (r *SQLRepository) ListRejudgeBatchItems(ctx context.Context, batchID int64) ([]RejudgeBatchItemRecord, error) {
	rows, err := r.q.ListRejudgeBatchItems(ctx, batchID)
	if err != nil {
		return nil, err
	}
	out := make([]RejudgeBatchItemRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, rejudgeBatchItemRecord(row))
	}
	return out, nil
}

func (r *SQLRepository) CancelRejudgeBatch(ctx context.Context, id int64, reason string) (RejudgeBatchRecord, error) {
	if r.txRunner == nil {
		return RejudgeBatchRecord{}, errors.New("transaction runner is required to cancel rejudge batch")
	}
	var batch RejudgeBatchRecord
	err := postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		current, err := q.GetRejudgeBatchByID(ctx, id)
		if err != nil {
			return mapNotFound(err, "rejudge.not_found", "rejudge batch not found")
		}
		items, err := q.CancelQueuedRejudgeBatchItems(ctx, db.CancelQueuedRejudgeBatchItemsParams{ErrorMessage: text(reason), BatchID: id})
		if err != nil {
			return err
		}
		for _, item := range items {
			if _, err := q.CancelPendingJudgeTaskForRejudge(ctx, db.CancelPendingJudgeTaskForRejudgeParams{LastError: text(reason), ID: item.TaskID}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return apperror.Conflict("rejudge.cancel_race", "a queued rejudge item started while cancellation was requested")
				}
				return err
			}
			if _, err := q.RestoreSubmissionAfterCanceledRejudge(ctx, item.SubmissionID); err != nil {
				return err
			}
		}
		row, err := q.CancelRejudgeBatch(ctx, db.CancelRejudgeBatchParams{
			CanceledCount: current.CanceledCount + int32(len(items)), ErrorMessage: text(reason), FinishedAt: timestamptz(time.Now().UTC()), ID: id,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperror.Conflict("rejudge.not_cancelable", "rejudge batch cannot be canceled")
			}
			return err
		}
		batch = rejudgeBatchRecord(row)
		return nil
	})
	return batch, err
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

func (r *SQLRepository) ListSubmissionsByCursor(ctx context.Context, input ListSubmissionsInput) ([]SubmissionRecord, error) {
	cursor := input.Cursor
	if cursor == nil {
		cursor = &SubmissionCursor{SubmittedAt: time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC), ID: 1<<63 - 1}
	}
	rows, err := r.q.ListSubmissionsByCursor(ctx, db.ListSubmissionsByCursorParams{
		UserID:            int8Ptr(input.UserID),
		ProblemID:         int8Ptr(input.ProblemID),
		ContestID:         int8Ptr(input.ContestID),
		Status:            textPtr(input.Status),
		BeforeSubmittedAt: pgtype.Timestamptz{Time: cursor.SubmittedAt.UTC(), Valid: true},
		BeforeID:          cursor.ID,
		Limit:             input.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]SubmissionRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, submissionRecord(row))
	}
	return out, nil
}

func (r *SQLRepository) ListSubmissionsByUserBefore(ctx context.Context, userID int64, cursor SubmissionCursor, limit int32) ([]SubmissionRecord, error) {
	rows, err := r.q.ListSubmissionsByUserBefore(ctx, db.ListSubmissionsByUserBeforeParams{
		UserID:            userID,
		BeforeSubmittedAt: pgtype.Timestamptz{Time: cursor.SubmittedAt, Valid: true},
		BeforeID:          cursor.ID,
		Limit:             limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]SubmissionRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, submissionRecord(row))
	}
	return out, nil
}

func (r *SQLRepository) ListSubmissionSummaries(ctx context.Context, submissionIDs []int64, includeAttempts bool) (map[int64]SubmissionListSummary, error) {
	summaries := make(map[int64]SubmissionListSummary, len(submissionIDs))
	if len(submissionIDs) == 0 {
		return summaries, nil
	}
	resultRows, err := r.q.ListSubmissionResultsBySubmissionIDs(ctx, submissionIDs)
	if err != nil {
		return nil, err
	}
	for _, row := range resultRows {
		result := submissionResultRecord(row)
		summary := summaries[result.SubmissionID]
		summary.Result = &result
		summaries[result.SubmissionID] = summary
	}
	if !includeAttempts {
		return summaries, nil
	}
	attemptRows, err := r.q.ListLatestJudgeAttemptsBySubmissionIDs(ctx, submissionIDs)
	if err != nil {
		return nil, err
	}
	for _, row := range attemptRows {
		attempt := judgeAttemptRecord(row)
		if attempt.SubmissionID == nil {
			continue
		}
		summary := summaries[*attempt.SubmissionID]
		summary.LatestAttempt = &attempt
		summaries[*attempt.SubmissionID] = summary
	}
	return summaries, nil
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
		current, err := q.GetSubmissionByID(ctx, id)
		if err != nil {
			return err
		}
		record = submissionRecord(current)
		if terminalStatus(record.Status) {
			return nil
		}
		projectionLock, err := lockContestProblemProjection(ctx, q, record)
		if err != nil {
			return err
		}
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
		if _, err := persistJudgeResult(ctx, q, record, result, score); err != nil {
			return err
		}
		return rebuildContestProblemResult(ctx, q, record, projectionLock)
	})
	return record, err
}

func (r *SQLRepository) EnsureJudgeAttempt(ctx context.Context, input EnsureJudgeAttemptInput) (JudgeAttemptRecord, error) {
	if r.txRunner == nil {
		return ensureJudgeAttempt(ctx, r.q, input)
	}
	var attempt JudgeAttemptRecord
	err := postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		var err error
		attempt, err = ensureJudgeAttempt(ctx, r.q.WithTx(tx), input)
		return err
	})
	return attempt, err
}

func ensureJudgeAttempt(ctx context.Context, q *db.Queries, input EnsureJudgeAttemptInput) (JudgeAttemptRecord, error) {
	if input.AttemptID != "" {
		if id, err := strconv.ParseInt(input.AttemptID, 10, 64); err == nil {
			row, err := q.GetJudgeAttemptByID(ctx, id)
			if err == nil {
				return judgeAttemptRecord(row), nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return JudgeAttemptRecord{}, err
			}
		}
	}
	item, itemErr := q.GetQueuedRejudgeBatchItemByTaskID(ctx, input.TaskID)
	if itemErr != nil && !errors.Is(itemErr, pgx.ErrNoRows) {
		return JudgeAttemptRecord{}, itemErr
	}
	latest, err := q.GetLatestJudgeAttemptBySubmissionID(ctx, pgtype.Int8{Int64: input.SubmissionID, Valid: true})
	attemptNo := int32(1)
	if err == nil {
		if latest.TaskID.Valid && latest.TaskID.Int64 == input.TaskID && !terminalStatus(latest.Status) {
			if itemErr == nil {
				if _, err := q.StartRejudgeBatchItem(ctx, db.StartRejudgeBatchItemParams{AttemptID: pgtype.Int8{Int64: latest.ID, Valid: true}, ID: item.ID}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return JudgeAttemptRecord{}, err
				}
			}
			return judgeAttemptRecord(latest), nil
		}
		attemptNo = latest.AttemptNo + 1
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return JudgeAttemptRecord{}, err
	}
	manifest, err := json.Marshal(map[string]any{
		"trace_id":          input.TraceID,
		"testcase_set_hash": input.TestcaseSetHash,
	})
	if err != nil {
		return JudgeAttemptRecord{}, err
	}
	startedAt := input.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	row, err := q.CreateJudgeAttempt(ctx, db.CreateJudgeAttemptParams{
		SubmissionID:     pgtype.Int8{Int64: input.SubmissionID, Valid: true},
		TaskID:           pgtype.Int8{Int64: input.TaskID, Valid: input.TaskID > 0},
		RejudgeBatchID:   pgtype.Int8{Int64: item.BatchID, Valid: itemErr == nil},
		AttemptNo:        attemptNo,
		ProtocolVersion:  input.ProtocolVersion,
		JudgeCoreVersion: input.ProtocolVersion,
		JudgeEngine:      input.JudgeEngine,
		LanguageID:       input.LanguageID,
		TestcaseSetID:    pgtype.Int8{Int64: input.TestcaseSetID, Valid: input.TestcaseSetID > 0},
		TestcaseSetHash:  text(input.TestcaseSetHash),
		Status:           "created",
		Score:            0,
		Manifest:         manifest,
		Metrics:          []byte(`{}`),
		TraceID:          text(input.TraceID),
		StartedAt:        timestamptz(startedAt),
	})
	if err != nil {
		return JudgeAttemptRecord{}, err
	}
	if itemErr == nil {
		if _, err := q.StartRejudgeBatchItem(ctx, db.StartRejudgeBatchItemParams{AttemptID: pgtype.Int8{Int64: row.ID, Valid: true}, ID: item.ID}); err != nil {
			return JudgeAttemptRecord{}, err
		}
		if _, err := q.RefreshRejudgeBatchProgress(ctx, item.BatchID); err != nil {
			return JudgeAttemptRecord{}, err
		}
	}
	return judgeAttemptRecord(row), nil
}

func (r *SQLRepository) CompleteJudgeAttemptResult(ctx context.Context, input CompleteJudgeAttemptResultInput) (SubmissionRecord, bool, error) {
	if r.txRunner == nil {
		return SubmissionRecord{}, false, errors.New("transaction runner is required to complete judge attempt result")
	}
	attemptID, err := strconv.ParseInt(input.AttemptKey, 10, 64)
	if err != nil {
		return SubmissionRecord{}, false, fmt.Errorf("invalid attempt_id %q: %w", input.AttemptKey, err)
	}
	score := int32(0)
	if input.Result.Verdict == judge.VerdictAccepted {
		score = 100
	}
	status := dbStatus(input.Status)

	var record SubmissionRecord
	var persisted bool
	err = postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		attempt, err := q.GetJudgeAttemptByID(ctx, attemptID)
		if err != nil {
			return err
		}
		if attempt.SubmissionID.Valid {
			submissionRow, err := q.GetSubmissionByID(ctx, attempt.SubmissionID.Int64)
			if err != nil {
				return err
			}
			record = submissionRecord(submissionRow)
		}
		if terminalStatus(attempt.Status) {
			return nil
		}
		if !attempt.SubmissionID.Valid {
			return fmt.Errorf("judge attempt %d is not linked to a submission", attempt.ID)
		}
		projectionLock, err := lockContestProblemProjection(ctx, q, record)
		if err != nil {
			return err
		}

		params := db.UpdateSubmissionStatusParams{
			Status:       status,
			TimeMs:       int4(input.Result.TimeMS),
			MemoryKb:     int4(input.Result.MemoryKB),
			Score:        pgtype.Int4{Int32: score, Valid: true},
			ErrorMessage: text(input.Result.ErrorMessage),
			ID:           attempt.SubmissionID.Int64,
		}
		submissionRow, err := q.UpdateSubmissionStatus(ctx, params)
		if errors.Is(err, pgx.ErrNoRows) {
			submissionRow, err = q.GetSubmissionByID(ctx, attempt.SubmissionID.Int64)
		}
		if err != nil {
			return err
		}
		record = submissionRecord(submissionRow)

		summary := judgeSummary(input.Result)
		manifest, err := judgeManifestJSON(input.Result.Manifest)
		if err != nil {
			return err
		}
		safeSummary, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		metrics, err := json.Marshal(map[string]any{})
		if err != nil {
			return err
		}
		finishedAt := input.Result.JudgedAt
		if finishedAt.IsZero() {
			finishedAt = time.Now().UTC()
		}
		finished, err := q.MarkJudgeAttemptFinished(ctx, db.MarkJudgeAttemptFinishedParams{
			ID:                   attempt.ID,
			Status:               status,
			Verdict:              text(status),
			Score:                score,
			TimeMs:               int4(input.Result.TimeMS),
			MemoryKb:             int4(input.Result.MemoryKB),
			FirstFailedCaseIndex: summary.firstFailedCaseIndex(),
			FirstFailedGroup:     text(summary.FirstFailedGroup),
			CompileOutputSummary: text(summary.CompileOutputSummary),
			StderrSummary:        text(summary.StderrSummary),
			CheckerMessage:       text(summary.CheckerMessage),
			ErrorClass:           text(summary.ErrorClass),
			ErrorMessage:         text(input.Result.ErrorMessage),
			Manifest:             manifest,
			Metrics:              metrics,
			TraceID:              text(input.TraceID),
			FinishedAt:           timestamptz(finishedAt),
		})
		if err != nil {
			return err
		}
		for i, item := range input.Result.Cases {
			index := item.Index
			if index == 0 {
				index = i + 1
			}
			_, err := q.CreateJudgeCaseResult(ctx, db.CreateJudgeCaseResultParams{
				AttemptID:         attempt.ID,
				CaseIndex:         int32(index),
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
				return err
			}
		}
		_, err = q.UpsertSubmissionResult(ctx, db.UpsertSubmissionResultParams{
			SubmissionID:         record.ID,
			AttemptID:            attempt.ID,
			Status:               status,
			Score:                score,
			TimeMs:               int4(input.Result.TimeMS),
			MemoryKb:             int4(input.Result.MemoryKB),
			FirstFailedCaseIndex: summary.firstFailedCaseIndex(),
			FirstFailedGroup:     text(summary.FirstFailedGroup),
			ErrorClass:           text(summary.ErrorClass),
			SafeSummary:          safeSummary,
		})
		if err != nil {
			return err
		}
		if err := rebuildContestProblemResult(ctx, q, record, projectionLock); err != nil {
			return err
		}
		if finished.TaskID.Valid {
			if _, err := q.MarkJudgeTaskDone(ctx, finished.TaskID.Int64); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		if finished.RejudgeBatchID.Valid {
			item, err := q.FinishRejudgeBatchItem(ctx, db.FinishRejudgeBatchItemParams{
				Status: RejudgeItemStatusCompleted, ErrorMessage: pgtype.Text{}, AttemptID: pgtype.Int8{Int64: finished.ID, Valid: true},
			})
			if err != nil {
				return err
			}
			if _, err := q.RefreshRejudgeBatchProgress(ctx, item.BatchID); err != nil {
				return err
			}
		}
		persisted = true
		return nil
	})
	return record, persisted, err
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

type contestProblemProjectionLock struct {
	contestStart time.Time
	enabled      bool
}

type contestProjectionSubmission struct {
	ID          int64
	Status      string
	SubmittedAt time.Time
	AttemptID   *int64
}

type contestProblemProjection struct {
	Status           string
	Attempts         int32
	AcceptedAt       *time.Time
	PenaltyMinutes   int32
	LastSubmissionID *int64
	BestSubmissionID *int64
	BestAttemptID    *int64
	LastAttemptID    *int64
}

func lockContestProblemProjection(ctx context.Context, q *db.Queries, submission SubmissionRecord) (contestProblemProjectionLock, error) {
	if submission.ContestID == nil {
		return contestProblemProjectionLock{}, nil
	}
	contest, err := q.GetContestByID(ctx, *submission.ContestID)
	if err != nil {
		return contestProblemProjectionLock{}, err
	}
	problems, err := q.ListContestProblems(ctx, *submission.ContestID)
	if err != nil {
		return contestProblemProjectionLock{}, err
	}
	for _, problem := range problems {
		if problem.ProblemID != submission.ProblemID {
			continue
		}
		params := db.EnsureContestProblemResultProjectionParams{
			ContestID: *submission.ContestID,
			UserID:    submission.UserID,
			ProblemID: submission.ProblemID,
		}
		if err := q.EnsureContestProblemResultProjection(ctx, params); err != nil {
			return contestProblemProjectionLock{}, err
		}
		if _, err := q.LockContestProblemResultProjection(ctx, db.LockContestProblemResultProjectionParams(params)); err != nil {
			return contestProblemProjectionLock{}, err
		}
		return contestProblemProjectionLock{contestStart: contest.StartAt.Time, enabled: true}, nil
	}
	return contestProblemProjectionLock{}, nil
}

func rebuildContestProblemResult(ctx context.Context, q *db.Queries, submission SubmissionRecord, lock contestProblemProjectionLock) error {
	if !lock.enabled || submission.ContestID == nil {
		return nil
	}
	rows, err := q.ListContestProblemSubmissionsForProjection(ctx, db.ListContestProblemSubmissionsForProjectionParams{
		ContestID: pgtype.Int8{Int64: *submission.ContestID, Valid: true},
		UserID:    submission.UserID,
		ProblemID: submission.ProblemID,
	})
	if err != nil {
		return err
	}
	items := make([]contestProjectionSubmission, 0, len(rows))
	for _, row := range rows {
		items = append(items, contestProjectionSubmission{
			ID:          row.ID,
			Status:      row.Status,
			SubmittedAt: row.SubmittedAt.Time,
			AttemptID:   int8Value(row.AttemptID),
		})
	}
	projection := buildContestProblemProjection(lock.contestStart, items)
	acceptedAt := pgtype.Timestamptz{}
	if projection.AcceptedAt != nil {
		acceptedAt = timestamptz(*projection.AcceptedAt)
	}
	_, err = q.UpsertContestProblemResult(ctx, db.UpsertContestProblemResultParams{
		ContestID:        *submission.ContestID,
		UserID:           submission.UserID,
		ProblemID:        submission.ProblemID,
		Status:           projection.Status,
		Attempts:         projection.Attempts,
		AcceptedAt:       acceptedAt,
		PenaltyMinutes:   projection.PenaltyMinutes,
		LastSubmissionID: int8Ptr(projection.LastSubmissionID),
		BestSubmissionID: int8Ptr(projection.BestSubmissionID),
		BestAttemptID:    int8Ptr(projection.BestAttemptID),
		LastAttemptID:    int8Ptr(projection.LastAttemptID),
	})
	return err
}

func buildContestProblemProjection(contestStart time.Time, submissions []contestProjectionSubmission) contestProblemProjection {
	ordered := append([]contestProjectionSubmission(nil), submissions...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SubmittedAt.Equal(ordered[j].SubmittedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].SubmittedAt.Before(ordered[j].SubmittedAt)
	})

	projection := contestProblemProjection{Status: "none"}
	for _, submission := range ordered {
		projection.Status = "attempted"
		projection.Attempts++
		lastSubmissionID := submission.ID
		projection.LastSubmissionID = &lastSubmissionID
		projection.LastAttemptID = copyInt64Ptr(submission.AttemptID)
		if submission.Status != StatusAccepted {
			continue
		}
		acceptedAt := submission.SubmittedAt
		projection.Status = StatusAccepted
		projection.AcceptedAt = &acceptedAt
		projection.PenaltyMinutes = int32(acceptedAt.Sub(contestStart).Minutes()) + (projection.Attempts-1)*20
		projection.BestSubmissionID = &lastSubmissionID
		projection.BestAttemptID = copyInt64Ptr(submission.AttemptID)
		break
	}
	return projection
}

func copyInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
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
	if r.txRunner == nil {
		row, err := r.q.MarkJudgeTaskDead(ctx, db.MarkJudgeTaskDeadParams{ID: id, LastError: text(reason)})
		return judgeTaskRecord(row), err
	}
	var task JudgeTaskRecord
	err := postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.MarkJudgeTaskDead(ctx, db.MarkJudgeTaskDeadParams{ID: id, LastError: text(reason)})
		if err != nil {
			return err
		}
		task = judgeTaskRecord(row)
		if _, err := q.MarkSubmissionSystemError(ctx, db.MarkSubmissionSystemErrorParams{ID: task.SubmissionID, ErrorMessage: text(reason)}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		item, err := q.FailActiveRejudgeBatchItemByTaskID(ctx, db.FailActiveRejudgeBatchItemByTaskIDParams{ErrorMessage: text(reason), TaskID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = q.RefreshRejudgeBatchProgress(ctx, item.BatchID)
		return err
	})
	return task, err
}

func (r *SQLRepository) RecoverDeadJudgeTask(ctx context.Context, id int64, nextRunAt time.Time, reason string) (JudgeTaskRecord, error) {
	row, err := r.q.RecoverDeadJudgeTask(ctx, db.RecoverDeadJudgeTaskParams{NextRunAt: timestamptz(nextRunAt), LastError: text(reason), ID: id})
	return recoverDeadJudgeTaskRecord(row), err
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

func recoverDeadJudgeTaskRecord(row db.RecoverDeadJudgeTaskRow) JudgeTaskRecord {
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

func rejudgeBatchRecord(row db.RejudgeBatch) RejudgeBatchRecord {
	return RejudgeBatchRecord{
		ID: row.ID, ProblemID: int8Value(row.ProblemID), ContestID: int8Value(row.ContestID), RequestedBy: row.RequestedBy,
		Status: row.Status, Reason: row.Reason, TotalCount: row.TotalCount, CompletedCount: row.CompletedCount,
		FailedCount: row.FailedCount, CanceledCount: row.CanceledCount, ErrorMessage: textValue(row.ErrorMessage),
		StartedAt: timeValue(row.StartedAt), FinishedAt: timeValue(row.FinishedAt), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
}

func rejudgeBatchItemRecord(row db.RejudgeBatchItem) RejudgeBatchItemRecord {
	return RejudgeBatchItemRecord{
		ID: row.ID, BatchID: row.BatchID, SubmissionID: row.SubmissionID, TaskID: row.TaskID, AttemptID: int8Value(row.AttemptID),
		Status: row.Status, ErrorMessage: textValue(row.ErrorMessage), StartedAt: timeValue(row.StartedAt), FinishedAt: timeValue(row.FinishedAt),
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
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
