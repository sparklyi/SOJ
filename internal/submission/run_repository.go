package submission

import (
	"context"
	"errors"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
)

func (r *SQLRepository) CreateRun(ctx context.Context, arg RunRecord) (RunRecord, error) {
	row, err := r.q.CreateRun(ctx, db.CreateRunParams{
		UserID:           arg.UserID,
		ProblemID:        int8Ptr(arg.ProblemID),
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
