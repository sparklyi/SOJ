package submission

import (
	"context"
	"errors"
	"sort"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/postgres"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

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
		type rejudgeTarget struct {
			submission db.Submission
			task       db.JudgeTask
		}
		targets := make([]rejudgeTarget, 0, len(submissions))
		for _, candidate := range submissions {
			task, err := q.GetJudgeTaskBySubmissionID(ctx, candidate.ID)
			if err != nil {
				return err
			}
			targets = append(targets, rejudgeTarget{submission: candidate, task: task})
		}
		sort.Slice(targets, func(i, j int) bool {
			if targets[i].task.ID != targets[j].task.ID {
				return targets[i].task.ID < targets[j].task.ID
			}
			return targets[i].submission.ID < targets[j].submission.ID
		})
		for i := range targets {
			lockedTask, err := q.LockJudgeTaskByID(ctx, targets[i].task.ID)
			if err != nil {
				return err
			}
			targets[i].task = lockedTask
		}
		sort.Slice(targets, func(i, j int) bool {
			if targets[i].submission.ID != targets[j].submission.ID {
				return targets[i].submission.ID < targets[j].submission.ID
			}
			return targets[i].task.ID < targets[j].task.ID
		})
		for i := range targets {
			lockedSubmission, err := q.LockSubmissionByID(ctx, targets[i].submission.ID)
			if err != nil {
				return err
			}
			targets[i].submission = lockedSubmission
			if !terminalStatus(lockedSubmission.Status) || (targets[i].task.Status != "done" && targets[i].task.Status != "dead") {
				return apperror.Conflict("rejudge.task_not_ready", "submission judge task is not done or dead")
			}
		}
		row, err := q.CreateRejudgeBatch(ctx, db.CreateRejudgeBatchParams{
			ProblemID: int8Ptr(input.ProblemID), ContestID: int8Ptr(input.ContestID), RequestedBy: input.RequestedBy,
			Status: RejudgeBatchStatusQueued, Reason: input.Reason, Filters: []byte(`{}`), TotalCount: int32(len(targets)),
		})
		if err != nil {
			return err
		}
		batch = rejudgeBatchRecord(row)
		for _, target := range targets {
			if _, err := q.CreateRejudgeBatchItem(ctx, db.CreateRejudgeBatchItemParams{BatchID: batch.ID, SubmissionID: target.submission.ID, TaskID: target.task.ID}); err != nil {
				return err
			}
			projectionLock, err := lockContestProblemProjection(ctx, q, submissionRecord(target.submission))
			if err != nil {
				return err
			}
			if _, err := q.PrepareJudgeTaskForRejudge(ctx, db.PrepareJudgeTaskForRejudgeParams{NextRunAt: timestamptz(input.NextRunAt), ID: target.task.ID, SubmissionID: target.submission.ID}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return apperror.Conflict("rejudge.task_not_ready", "submission judge task is not done or dead")
				}
				return err
			}
			if _, err := q.PrepareSubmissionForRejudge(ctx, target.submission.ID); err != nil {
				return err
			}
			if err := rebuildContestProblemResult(ctx, q, submissionRecord(target.submission), projectionLock); err != nil {
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
		allItems, err := q.ListRejudgeBatchItems(ctx, id)
		if err != nil {
			return err
		}
		queuedItems := make([]db.RejudgeBatchItem, 0, len(allItems))
		for _, item := range allItems {
			if item.Status == RejudgeItemStatusQueued {
				queuedItems = append(queuedItems, item)
			}
		}
		sort.Slice(queuedItems, func(i, j int) bool {
			if queuedItems[i].TaskID != queuedItems[j].TaskID {
				return queuedItems[i].TaskID < queuedItems[j].TaskID
			}
			return queuedItems[i].ID < queuedItems[j].ID
		})
		for _, item := range queuedItems {
			if _, err := q.LockJudgeTaskByID(ctx, item.TaskID); err != nil {
				return err
			}
		}
		sort.Slice(queuedItems, func(i, j int) bool {
			if queuedItems[i].SubmissionID != queuedItems[j].SubmissionID {
				return queuedItems[i].SubmissionID < queuedItems[j].SubmissionID
			}
			return queuedItems[i].ID < queuedItems[j].ID
		})
		lockedSubmissions := make(map[int64]db.Submission, len(queuedItems))
		for _, item := range queuedItems {
			submissionRow, err := q.LockSubmissionByID(ctx, item.SubmissionID)
			if err != nil {
				return err
			}
			lockedSubmissions[item.SubmissionID] = submissionRow
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
			submissionRow := lockedSubmissions[item.SubmissionID]
			projectionLock, err := lockContestProblemProjection(ctx, q, submissionRecord(submissionRow))
			if err != nil {
				return err
			}
			restored, err := q.RestoreSubmissionAfterCanceledRejudge(ctx, item.SubmissionID)
			if err != nil {
				return err
			}
			if err := rebuildContestProblemResult(ctx, q, submissionRecord(restored), projectionLock); err != nil {
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
