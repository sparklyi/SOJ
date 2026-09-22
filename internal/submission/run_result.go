package submission

import (
	"context"

	"SOJ/internal/postgres/db"
)

// completeRunAttempt writes a finished self-run. It is the run counterpart of
// completeSubmissionAttempt: the attempt and task bookkeeping is shared, and all
// that differs is which row the verdict lands on.
//
// A run has no testcases, no score and no contest projection, so none of that
// appears here. Its stdout, stderr and compile output are the payload.
func completeRunAttempt(ctx context.Context, q *db.Queries, attempt db.JudgeAttempt, input CompleteJudgeAttemptResultInput, status string, persisted *bool) error {
	runRow, err := q.LockRunByID(ctx, attempt.RunID.Int64)
	if err != nil {
		return err
	}
	if terminalStatus(runRow.Status) {
		// The result stream is at-least-once. Re-delivery must not rewrite a
		// finished run.
		return nil
	}
	if _, err := q.UpdateRunStatus(ctx, db.UpdateRunStatusParams{
		Status:        status,
		Stdout:        text(input.Result.Stdout),
		Stderr:        text(input.Result.Stderr),
		CompileOutput: text(input.Result.CompileOutput),
		TimeMs:        int4(input.Result.TimeMS),
		MemoryKb:      int4(input.Result.MemoryKB),
		ErrorMessage:  text(input.Result.ErrorMessage),
		ID:            attempt.RunID.Int64,
	}); err != nil {
		return err
	}
	if _, err := finishJudgeAttempt(ctx, q, attempt, input, status); err != nil {
		return err
	}
	*persisted = true
	return nil
}
