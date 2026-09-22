package submission

import (
	"context"
	"errors"
	"time"

	"SOJ/internal/postgres"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
)

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
		return JudgeTaskRecord{}, errors.New("transaction runner is required to mark judge task dead")
	}
	var task JudgeTaskRecord
	err := postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.MarkJudgeTaskDead(ctx, db.MarkJudgeTaskDeadParams{ID: id, LastError: text(reason)})
		if err != nil {
			return err
		}
		task = judgeTaskRecord(row)
		if _, err := markSubmissionSystemError(ctx, q, task.SubmissionID, reason); err != nil {
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
	if r.txRunner == nil {
		return JudgeTaskRecord{}, errors.New("transaction runner is required to recover dead judge task")
	}
	var task JudgeTaskRecord
	err := postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		currentTask, err := q.LockJudgeTaskByID(ctx, id)
		if err != nil {
			return err
		}
		submissionRow, err := q.LockSubmissionByID(ctx, currentTask.SubmissionID)
		if err != nil {
			return err
		}
		projectionLock, err := lockContestProblemProjection(ctx, q, submissionRecord(submissionRow))
		if err != nil {
			return err
		}
		row, err := q.RecoverDeadJudgeTask(ctx, db.RecoverDeadJudgeTaskParams{NextRunAt: timestamptz(nextRunAt), LastError: text(reason), ID: id})
		if err != nil {
			return err
		}
		task = recoverDeadJudgeTaskRecord(row)
		if !projectionLock.enabled {
			return nil
		}
		updated, err := q.GetSubmissionByID(ctx, currentTask.SubmissionID)
		if err != nil {
			return err
		}
		return rebuildContestProblemResult(ctx, q, submissionRecord(updated), projectionLock)
	})
	return task, err
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

func judgeTaskRecord(row db.JudgeTask) JudgeTaskRecord {
	return JudgeTaskRecord{ID: row.ID, SubmissionID: row.SubmissionID, StreamID: row.StreamID.String, Status: row.Status, Attempts: row.Attempts, NextRunAt: row.NextRunAt.Time, LastError: row.LastError.String}
}

func resetJudgeTaskRecord(row db.ResetStaleJudgeTasksRow) JudgeTaskRecord {
	return JudgeTaskRecord{ID: row.ID, SubmissionID: row.SubmissionID, StreamID: row.StreamID.String, Status: row.Status, Attempts: row.Attempts, NextRunAt: row.NextRunAt.Time, LastError: row.LastError.String}
}

func recoverDeadJudgeTaskRecord(row db.RecoverDeadJudgeTaskRow) JudgeTaskRecord {
	return JudgeTaskRecord{ID: row.ID, SubmissionID: row.SubmissionID, StreamID: row.StreamID.String, Status: row.Status, Attempts: row.Attempts, NextRunAt: row.NextRunAt.Time, LastError: row.LastError.String}
}
