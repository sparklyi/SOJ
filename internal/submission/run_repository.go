package submission

import (
	"context"
	"errors"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/judge"
	"SOJ/internal/postgres"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
)

// AdmitRunInput is a run to persist, together with the rules that decide whether
// it may exist at all.
//
// There is deliberately no plain CreateRun next to this: a second way to insert a
// run would be a way to insert one that skipped the cap.
type AdmitRunInput struct {
	Run RunRecord
	// MaxActive caps how many runs the user may have in flight. In flight means
	// not yet terminal: the number of rows the user is still waiting on, not the
	// number of requests currently in progress.
	MaxActive int
	// Enqueue creates the judge task in the same transaction. A queued run with
	// no task would sit until the reconciler gave up on it half an hour later.
	Enqueue   bool
	NextRunAt time.Time
}

// AdmitRun persists a new run and enforces the per-user in-flight cap.
//
// The cap is counted in the database rather than in memory. An in-process
// counter measures the wrong thing once execution moves to the judge-agent -- the
// HTTP request returns long before the run finishes -- and across API replicas it
// would silently allow N times the configured value. Counting rows is exact for
// both, and locking the user row serialises admission so the count cannot race
// the insert.
func (r *SQLRepository) AdmitRun(ctx context.Context, input AdmitRunInput) (RunRecord, error) {
	if r.txRunner == nil {
		return RunRecord{}, errors.New("transaction runner is required to admit a run")
	}
	var run RunRecord
	err := postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if input.MaxActive > 0 {
			if _, err := q.LockUserForRunAdmission(ctx, input.Run.UserID); err != nil {
				// A token can outlive its user. Reporting it as a missing user
				// beats surfacing a raw pgx no-rows error.
				return mapNotFound(err, "user.not_found", "user not found")
			}
			active, err := q.CountActiveRunsByUser(ctx, input.Run.UserID)
			if err != nil {
				return err
			}
			if active >= int64(input.MaxActive) {
				return apperror.TooManyRequests("run.user_limit_exceeded", "too many runs in flight for this user")
			}
		}
		row, err := q.CreateRun(ctx, db.CreateRunParams{
			UserID:           input.Run.UserID,
			ProblemID:        int8Ptr(input.Run.ProblemID),
			LanguageID:       input.Run.LanguageID,
			Status:           input.Run.Status,
			SourceArtifactID: validInt8(input.Run.SourceArtifactID),
			Stdin:            text(input.Run.Stdin),
		})
		if err != nil {
			return err
		}
		if input.Enqueue {
			if _, err := q.CreateJudgeTask(ctx, db.CreateJudgeTaskParams{
				RunID:     validInt8(row.ID),
				Status:    "pending",
				NextRunAt: timestamptz(input.NextRunAt),
			}); err != nil {
				return err
			}
		}
		run = runRecord(row)
		return nil
	})
	return run, err
}

func (r *SQLRepository) GetRun(ctx context.Context, id int64) (RunRecord, error) {
	row, err := r.q.GetRunByID(ctx, id)
	return runRecord(row), mapNotFound(err, "run.not_found", "run not found")
}

// MarkRunRunning moves a queued run to running when it is handed to the agent.
// The status guard makes it a no-op on redelivery, so a retried dispatch cannot
// walk a finished run backwards.
func (r *SQLRepository) MarkRunRunning(ctx context.Context, id int64) (RunRecord, error) {
	row, err := r.q.MarkRunRunning(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.GetRun(ctx, id)
	}
	return runRecord(row), err
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

func runRecord(row db.Run) RunRecord {
	return RunRecord{ID: row.ID, UserID: row.UserID, ProblemID: int8Value(row.ProblemID), LanguageID: row.LanguageID, Status: row.Status, SourceArtifactID: row.SourceArtifactID.Int64, Stdin: row.Stdin.String, Stdout: row.Stdout.String, Stderr: row.Stderr.String, CompileOutput: row.CompileOutput.String, TimeMS: int4Value(row.TimeMs), MemoryKB: int4Value(row.MemoryKb), ErrorMessage: textValue(row.ErrorMessage), CreatedAt: row.CreatedAt.Time, FinishedAt: timeValue(row.FinishedAt), UpdatedAt: row.UpdatedAt.Time}
}
