package submission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/postgres"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

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
	latest, err := latestAttemptForSubject(ctx, q, input)
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
		SubmissionID:     int8Ptr(input.SubmissionID),
		RunID:            int8Ptr(input.RunID),
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

func (r *SQLRepository) CompleteJudgeAttemptResult(ctx context.Context, input CompleteJudgeAttemptResultInput) (bool, error) {
	if r.txRunner == nil {
		return false, errors.New("transaction runner is required to complete judge attempt result")
	}
	attemptID, err := strconv.ParseInt(input.AttemptKey, 10, 64)
	if err != nil {
		return false, fmt.Errorf("invalid attempt_id %q: %w", input.AttemptKey, err)
	}
	status := dbStatus(input.Status)

	var persisted bool
	err = postgres.WithTx(ctx, r.txRunner, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		attemptRef, err := q.GetJudgeAttemptByID(ctx, attemptID)
		if err != nil {
			return err
		}
		if attemptRef.TaskID.Valid {
			if _, err := q.LockJudgeTaskByID(ctx, attemptRef.TaskID.Int64); err != nil {
				return err
			}
		}
		attempt, err := q.LockJudgeAttemptByID(ctx, attemptID)
		if err != nil {
			return err
		}
		if terminalStatus(attempt.Status) {
			return nil
		}
		// A run and a submission publish the same result event shape. Which table
		// receives it follows from the attempt's subject, so the wire protocol
		// never needs a discriminator.
		switch {
		case attempt.RunID.Valid:
			return completeRunAttempt(ctx, q, attempt, input, status, &persisted)
		case attempt.SubmissionID.Valid:
			return completeSubmissionAttempt(ctx, q, attempt, input, status, &persisted)
		default:
			return fmt.Errorf("judge attempt %d is linked to neither a submission nor a run", attempt.ID)
		}
	})
	return persisted, err
}

// completeSubmissionAttempt writes a judged submission: the submission row, its
// result projection and the contest projections that depend on it.
func completeSubmissionAttempt(ctx context.Context, q *db.Queries, attempt db.JudgeAttempt, input CompleteJudgeAttemptResultInput, status string, persisted *bool) error {
	submissionRow, err := q.LockSubmissionByID(ctx, attempt.SubmissionID.Int64)
	if err != nil {
		return err
	}
	record := submissionRecord(submissionRow)
	if terminalStatus(record.Status) {
		return nil
	}
	projectionLock, err := lockContestProblemProjection(ctx, q, record)
	if err != nil {
		return err
	}

	score := int32(0)
	if input.Result.Verdict == judge.VerdictAccepted {
		score = 100
	}
	params := db.UpdateSubmissionStatusParams{
		Status:       status,
		TimeMs:       int4(input.Result.TimeMS),
		MemoryKb:     int4(input.Result.MemoryKB),
		Score:        pgtype.Int4{Int32: score, Valid: true},
		ErrorMessage: text(input.Result.ErrorMessage),
		JudgedAt:     judgedAtParam(input.Result.JudgedAt),
		ID:           attempt.SubmissionID.Int64,
	}
	submissionRow, err = q.UpdateSubmissionStatus(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		submissionRow, err = q.GetSubmissionByID(ctx, attempt.SubmissionID.Int64)
		if err == nil {
			record = submissionRecord(submissionRow)
			if terminalStatus(record.Status) {
				return nil
			}
		}
	}
	if err != nil {
		return err
	}
	record = submissionRecord(submissionRow)

	finished, err := finishJudgeAttempt(ctx, q, attempt, input, status)
	if err != nil {
		return err
	}
	summary := judgeSummary(input.Result)
	safeSummary, err := json.Marshal(summary)
	if err != nil {
		return err
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
	*persisted = true
	return nil
}

// finishJudgeAttempt closes the attempt itself: its own row, its per-case rows
// and the task that produced it. Every subject needs exactly this, so it lives
// once.
func finishJudgeAttempt(ctx context.Context, q *db.Queries, attempt db.JudgeAttempt, input CompleteJudgeAttemptResultInput, status string) (db.JudgeAttempt, error) {
	summary := judgeSummary(input.Result)
	manifest, err := judgeManifestJSON(input.Result.Manifest)
	if err != nil {
		return db.JudgeAttempt{}, err
	}
	metrics, err := json.Marshal(map[string]any{})
	if err != nil {
		return db.JudgeAttempt{}, err
	}
	finishedAt := input.Result.JudgedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	score := int32(0)
	if input.Result.Verdict == judge.VerdictAccepted {
		score = 100
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
		return db.JudgeAttempt{}, err
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
			return db.JudgeAttempt{}, err
		}
	}
	if finished.TaskID.Valid {
		if _, err := q.MarkJudgeTaskDone(ctx, finished.TaskID.Int64); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return db.JudgeAttempt{}, err
		}
	}
	return finished, nil
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
		Status:               dbStatus(result.Verdict),
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

// latestAttemptForSubject finds the attempt a new one would follow, so attempt
// numbering and in-flight reuse stay correct. Which column identifies the
// subject is the only thing that differs between a submission and a run.
func latestAttemptForSubject(ctx context.Context, q *db.Queries, input EnsureJudgeAttemptInput) (db.JudgeAttempt, error) {
	switch {
	case input.SubmissionID != nil:
		return q.GetLatestJudgeAttemptBySubmissionID(ctx, validInt8(*input.SubmissionID))
	case input.RunID != nil:
		return q.GetLatestJudgeAttemptByRunID(ctx, validInt8(*input.RunID))
	default:
		return db.JudgeAttempt{}, errors.New("ensure judge attempt requires a submission or a run")
	}
}
