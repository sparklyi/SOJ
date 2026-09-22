package submission

import (
	"context"
	"errors"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/postgres"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

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
			SubmissionID: validInt8(submission.ID),
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
	if r.txRunner == nil {
		return SubmissionRecord{}, errors.New("transaction runner is required to mark submission system_error")
	}
	var record SubmissionRecord
	err := postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		var err error
		record, err = markSubmissionSystemError(ctx, r.q.WithTx(tx), id, reason)
		return err
	})
	return record, err
}

func markSubmissionSystemError(ctx context.Context, q *db.Queries, id int64, reason string) (SubmissionRecord, error) {
	current, err := q.LockSubmissionByID(ctx, id)
	if err != nil {
		return SubmissionRecord{}, err
	}
	record := submissionRecord(current)
	if terminalStatus(record.Status) {
		return record, nil
	}
	projectionLock, err := lockContestProblemProjection(ctx, q, record)
	if err != nil {
		return SubmissionRecord{}, err
	}
	row, err := q.MarkSubmissionSystemError(ctx, db.MarkSubmissionSystemErrorParams{ID: id, ErrorMessage: text(reason)})
	if errors.Is(err, pgx.ErrNoRows) {
		return record, nil
	}
	if err != nil {
		return SubmissionRecord{}, err
	}
	record = submissionRecord(row)
	if err := rebuildContestProblemResult(ctx, q, record, projectionLock); err != nil {
		return SubmissionRecord{}, err
	}
	return record, nil
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
		JudgedAt:     judgedAtParam(result.JudgedAt),
		ID:           id,
	}

	var record SubmissionRecord
	err := postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		current, err := q.LockSubmissionByID(ctx, id)
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

func copyInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func submissionRecord(row db.Submission) SubmissionRecord {
	return SubmissionRecord{ID: row.ID, UserID: row.UserID, ProblemID: row.ProblemID, ContestID: int8Value(row.ContestID), LanguageID: row.LanguageID, TestcaseSetID: row.TestcaseSetID, Status: row.Status, SourceArtifactID: row.SourceArtifactID.Int64, TimeMS: int4Value(row.TimeMs), MemoryKB: int4Value(row.MemoryKb), Score: row.Score, ErrorMessage: textValue(row.ErrorMessage), SubmittedAt: row.SubmittedAt.Time, JudgedAt: timeValue(row.JudgedAt), FirstJudgedAt: timeValue(row.FirstJudgedAt), UpdatedAt: row.UpdatedAt.Time}
}
