package submission

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"SOJ/internal/auth"
	"SOJ/internal/authz"
	"SOJ/internal/judge"
	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type outcomeSavepointRunner struct{ tx pgx.Tx }

func (r outcomeSavepointRunner) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return r.tx.Begin(ctx)
}

func TestCreateRejudgeBatchLooksUpTasksOnce(t *testing.T) {
	tests := []struct {
		name       string
		contest    bool
		omitTask   bool
		selectName string
	}{
		{name: "problem", selectName: "ListEligibleProblemSubmissionsForRejudge"},
		{name: "contest", contest: true, selectName: "ListEligibleContestSubmissionsForRejudge"},
		{name: "missing task", omitTask: true, selectName: "ListEligibleProblemSubmissionsForRejudge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := beginOutcomeTestTx(t)
			ctx := t.Context()
			fixture := seedOutcomeFixture(t, tx)
			input := CreateRejudgeBatchRecordInput{ProblemID: &fixture.problemID, RequestedBy: fixture.userID, Reason: "integration", NextRunAt: time.Now()}
			if tt.contest {
				contestID := outcomeQueryID(t, tx, `INSERT INTO contests (owner_user_id, title, visibility, start_at, end_at, freeze_at)
					VALUES ($1, 'Rejudge test', 'private', now(), now() + interval '2 hours', now() + interval '1 hour') RETURNING id`, fixture.userID)
				input.ProblemID, input.ContestID = nil, &contestID
			}
			seedJudged := func() int64 {
				t.Helper()
				id := outcomeQueryID(t, tx, `INSERT INTO submissions (user_id, problem_id, contest_id, language_id, testcase_set_id, source_artifact_id)
					VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
					fixture.userID, fixture.problemID, input.ContestID, fixture.languageID, fixture.setID, fixture.artifactID)
				attemptID := outcomeQueryID(t, tx, `INSERT INTO judge_attempts (submission_id, attempt_no, protocol_version, judge_core_version, judge_engine, language_id, status)
					VALUES ($1, 1, 'integration', 'integration', 'integration', $2, 'accepted') RETURNING id`, id, fixture.languageID)
				return outcomeQueryID(t, tx, `UPDATE submissions SET status = 'judged', effective_attempt_id = $2 WHERE id = $1 RETURNING id`, id, attemptID)
			}
			first, second := seedJudged(), seedJudged()
			// Task IDs and submission IDs run in opposite orders.
			secondTask := outcomeQueryID(t, tx, `INSERT INTO judge_tasks (submission_id, status) VALUES ($1, 'done') RETURNING id`, second)
			var firstTask int64
			if !tt.omitTask {
				firstTask = outcomeQueryID(t, tx, `INSERT INTO judge_tasks (submission_id, status) VALUES ($1, 'dead') RETURNING id`, first)
			}

			runner := &rejudgeQueryRecorder{outcomeSavepointRunner: outcomeSavepointRunner{tx: tx}}
			repo := NewSQLRepositoryWithTxRunner(db.New(tx), runner)
			batch, err := repo.CreateRejudgeBatchWithItems(ctx, input)
			wantQueries := []rejudgeStatement{{name: tt.selectName}, {name: "ListJudgeTasksBySubmissionIDs"}}
			if !tt.omitTask {
				wantQueries = append(wantQueries,
					rejudgeStatement{name: "LockJudgeTaskByID", id: secondTask},
					rejudgeStatement{name: "LockJudgeTaskByID", id: firstTask},
					rejudgeStatement{name: "LockSubmissionByID", id: first},
					rejudgeStatement{name: "LockSubmissionByID", id: second},
				)
			}
			if !slices.Equal(runner.statements, wantQueries) {
				t.Errorf("selection, lookups and locks = %+v, want %+v", runner.statements, wantQueries)
			}
			if tt.omitTask {
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Errorf("missing task error = %v, want %v", err, pgx.ErrNoRows)
				}
				var batches, items int
				if err := tx.QueryRow(ctx, `SELECT
					(SELECT count(*) FROM rejudge_batches WHERE requested_by = $1),
					(SELECT count(*) FROM rejudge_batch_items WHERE submission_id IN ($2, $3))`,
					fixture.userID, first, second).Scan(&batches, &items); err != nil {
					t.Fatal(err)
				}
				if batches != 0 || items != 0 {
					t.Errorf("persisted %d batches and %d items after missing task, want none", batches, items)
				}
				return
			}
			if err != nil || batch.TotalCount != 2 {
				t.Fatalf("create rejudge batch: %+v, err = %v", batch, err)
			}
			items, err := repo.ListRejudgeBatchItems(ctx, batch.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 2 || items[0].SubmissionID != first || items[0].TaskID != firstTask ||
				items[1].SubmissionID != second || items[1].TaskID != secondTask {
				t.Errorf("batch items = %+v, want submission/task pairs %d/%d then %d/%d", items, first, firstTask, second, secondTask)
			}
		})
	}
}

type rejudgeStatement struct {
	name string
	id   int64
}

type rejudgeQueryRecorder struct {
	outcomeSavepointRunner
	statements []rejudgeStatement
}

func (r *rejudgeQueryRecorder) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := r.outcomeSavepointRunner.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return rejudgeRecordingTx{Tx: tx, recorder: r}, nil
}

type rejudgeRecordingTx struct {
	pgx.Tx
	recorder *rejudgeQueryRecorder
}

func (tx rejudgeRecordingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	name, _, _ := strings.Cut(strings.TrimPrefix(sql, "-- name: "), " ")
	switch name {
	case "ListEligibleProblemSubmissionsForRejudge", "ListEligibleContestSubmissionsForRejudge":
		tx.recorder.statements = append(tx.recorder.statements, rejudgeStatement{name: name})
	case "ListJudgeTasksBySubmissionIDs":
		tx.recorder.statements = append(tx.recorder.statements, rejudgeStatement{name: name})
		// Make the lookup order differ from the selected submission order.
		sql = strings.TrimSuffix(strings.TrimSpace(sql), ";") + " ORDER BY submission_id DESC"
	}
	return tx.Tx.Query(ctx, sql, args...)
}

func (tx rejudgeRecordingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	name, _, _ := strings.Cut(strings.TrimPrefix(sql, "-- name: "), " ")
	switch name {
	case "GetJudgeTaskBySubmissionID":
		tx.recorder.statements = append(tx.recorder.statements, rejudgeStatement{name: name})
	case "LockJudgeTaskByID", "LockSubmissionByID":
		tx.recorder.statements = append(tx.recorder.statements, rejudgeStatement{name: name, id: args[0].(int64)})
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func beginOutcomeTestTx(t *testing.T) pgx.Tx {
	t.Helper()
	dsn := os.Getenv("SOJ_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("SOJ_TEST_DATABASE_DSN is not set")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

func outcomeQueryID(t *testing.T, tx pgx.Tx, sql string, args ...any) int64 {
	t.Helper()
	var id int64
	if err := tx.QueryRow(t.Context(), sql, args...).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

type outcomeFixture struct {
	userID, problemID, languageID, setID, artifactID int64
}

func seedOutcomeFixture(t *testing.T, tx pgx.Tx) outcomeFixture {
	t.Helper()
	token := "outcome-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := outcomeQueryID(t, tx, `INSERT INTO users (email, password_hash, username) VALUES ($1, 'hash', $2) RETURNING id`, token+"@example.test", token)
	problemID := outcomeQueryID(t, tx, `INSERT INTO problems (owner_user_id, title, slug, difficulty, visibility, time_limit_ms, memory_limit_kb)
		VALUES ($1, 'Outcome test', $2, 'easy', 'public', 1000, 262144) RETURNING id`, userID, token)
	languageID := outcomeQueryID(t, tx, `INSERT INTO languages (engine, engine_language_id, name, default_time_limit_ms, default_memory_limit_kb)
		VALUES ('soj-agent', $1, 'Outcome test', 1000, 262144) RETURNING id`, token)
	setID := outcomeQueryID(t, tx, `INSERT INTO testcase_sets (problem_id, version, storage_key, checksum_sha256, size_bytes, case_count, created_by)
		VALUES ($1, 1, $2, repeat('a', 64), 1, 1, $3) RETURNING id`, problemID, token, userID)
	artifactID := outcomeQueryID(t, tx, `INSERT INTO artifacts (owner_type, owner_id, kind, storage_key, checksum_sha256, size_bytes, content_type)
		VALUES ('submission', $1, 'source', $2, repeat('b', 64), 1, 'text/plain') RETURNING id`, userID, token+"/source")
	return outcomeFixture{userID: userID, problemID: problemID, languageID: languageID, setID: setID, artifactID: artifactID}
}

func TestEffectiveSubmissionOutcomeAcrossRejudge(t *testing.T) {
	tx := beginOutcomeTestTx(t)
	ctx := t.Context()
	fixture := seedOutcomeFixture(t, tx)
	userID, problemID, languageID, setID := fixture.userID, fixture.problemID, fixture.languageID, fixture.setID
	queries := db.New(tx)
	repo := NewSQLRepositoryWithTxRunner(queries, outcomeSavepointRunner{tx})
	now := time.Unix(1770000000, 0).UTC()
	record, task, err := repo.CreateSubmissionWithTask(ctx, SubmissionRecord{
		UserID: userID, ProblemID: problemID, LanguageID: languageID, TestcaseSetID: setID, Status: StatusQueued, SourceArtifactID: fixture.artifactID,
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	actor := auth.Actor{UserID: userID, Role: auth.RoleRoot, Permissions: []auth.Permission{authz.PermissionJudgeInspect}}
	reader := NewSubmissionReader(repo, nil, nil)
	read := func() SubmissionView {
		t.Helper()
		view, err := reader.GetSubmission(ctx, actor, record.ID)
		if err != nil {
			t.Fatal(err)
		}
		return view
	}
	start := func() JudgeAttemptRecord {
		t.Helper()
		if _, err := repo.MarkJudgeTaskDispatching(ctx, task.ID); err != nil {
			t.Fatal(err)
		}
		attempt, err := repo.EnsureJudgeAttempt(ctx, EnsureJudgeAttemptInput{
			TaskID: task.ID, SubmissionID: &record.ID, LanguageID: languageID, TestcaseSetID: setID,
			ProtocolVersion: judgeevents.RequestEventType, JudgeEngine: judge.EngineSOJAgent, StartedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		return attempt
	}
	finish := func(attempt JudgeAttemptRecord, result judge.Result, wantPersisted bool) {
		t.Helper()
		persisted, err := repo.CompleteJudgeAttemptResult(ctx, CompleteJudgeAttemptResultInput{
			AttemptKey: strconv.FormatInt(attempt.ID, 10), Status: result.Verdict, Result: result,
		})
		if err != nil || persisted != wantPersisted {
			t.Fatalf("complete attempt %d: persisted=%v, want %v, err=%v", attempt.ID, persisted, wantPersisted, err)
		}
	}
	rejudge := func() RejudgeBatchRecord {
		t.Helper()
		batch, err := repo.CreateRejudgeBatchWithItems(ctx, CreateRejudgeBatchRecordInput{ProblemID: &problemID, RequestedBy: userID, Reason: "integration", NextRunAt: now})
		if err != nil {
			t.Fatal(err)
		}
		return batch
	}
	assertWorkflow := func(status string, attemptID int64) {
		t.Helper()
		var storedStatus string
		var storedID int64
		if err := tx.QueryRow(ctx, `SELECT status, effective_attempt_id FROM submissions WHERE id = $1`, record.ID).Scan(&storedStatus, &storedID); err != nil {
			t.Fatal(err)
		}
		if storedStatus != status || storedID != attemptID {
			t.Fatalf("stored phase/attempt = %q/%d, want %q/%d", storedStatus, storedID, status, attemptID)
		}
	}

	first := start()
	finish(first, judge.Result{
		Verdict: judge.VerdictWrongAnswer, JudgedAt: now, TimeMS: 17, MemoryKB: 512,
		Cases:    []judge.CaseResult{{Index: 1, Verdict: judge.VerdictWrongAnswer, OutputDiffSummary: "expected 1; got 0"}},
		Manifest: judge.Manifest{JudgeCoreVersion: "current-core", LanguageRuntime: "go@1.26", SandboxBackend: "fake"},
	}, true)
	assertWorkflow(statusJudged, first.ID)
	view := read()
	if view.Submission.Status != StatusWrongAnswer || view.Submission.TimeMS == nil || *view.Submission.TimeMS != 17 ||
		view.Result == nil || view.Result.AttemptID != first.ID || !bytes.Contains(view.Result.SafeSummary, []byte("expected 1; got 0")) ||
		view.AdminDiagnostics == nil || view.AdminDiagnostics.ID != first.ID || view.AdminDiagnostics.JudgeCoreVersion != "current-core" || len(view.Cases) != 1 || view.Cases[0].AttemptID != first.ID {
		t.Fatalf("first outcome = %+v", view)
	}

	batch := rejudge()
	assertWorkflow(StatusQueued, first.ID)
	if view := read(); view.Submission.Status != StatusQueued || view.Result != nil || view.Submission.TimeMS != nil || len(view.Cases) != 0 || view.AdminDiagnostics != nil {
		t.Fatalf("queued rejudge exposed previous result: %+v", view)
	}
	if batch, err := repo.CancelRejudgeBatch(ctx, batch.ID, "cancel before dispatch"); err != nil || batch.Status != RejudgeBatchStatusCanceled {
		t.Fatalf("cancel batch: %+v, err=%v", batch, err)
	}
	assertWorkflow(statusJudged, first.ID)
	view = read()
	if view.Result == nil || view.Result.AttemptID != first.ID || view.Submission.JudgedAt == nil || !view.Submission.JudgedAt.Equal(now) {
		t.Fatalf("cancel did not restore effective outcome: %+v", view)
	}

	batch = rejudge()
	second := start()
	secondResult := judge.Result{Verdict: judge.VerdictAccepted, JudgedAt: now.Add(time.Minute), TimeMS: 26}
	finish(second, secondResult, true)
	finish(second, judge.Result{Verdict: judge.VerdictWrongAnswer, TimeMS: 999}, false)
	finish(first, judge.Result{Verdict: judge.VerdictWrongAnswer, TimeMS: 999}, false)
	assertWorkflow(statusJudged, second.ID)
	view = read()
	if view.Submission.Status != StatusAccepted || view.Submission.TimeMS == nil || *view.Submission.TimeMS != 26 ||
		view.Result == nil || view.Result.AttemptID != second.ID || view.AdminDiagnostics == nil || view.AdminDiagnostics.ID != second.ID || len(view.Cases) != 0 ||
		view.Submission.FirstJudgedAt == nil || !view.Submission.FirstJudgedAt.Equal(now) {
		t.Fatalf("final outcome was not atomically replaced: %+v", view)
	}
	finishedBatch, err := repo.GetRejudgeBatch(ctx, batch.ID)
	if err != nil || finishedBatch.CompletedCount != 1 || finishedBatch.Status != RejudgeBatchStatusCompleted {
		t.Fatalf("completed batch: %+v, err=%v", finishedBatch, err)
	}
	// A newer non-effective attempt must never replace public cases/diagnostics.
	_ = outcomeQueryID(t, tx, `INSERT INTO judge_attempts (submission_id, attempt_no, protocol_version, judge_core_version, judge_engine, language_id, status)
		VALUES ($1, $2, 'later-protocol', 'later-core', 'later-engine', $3, 'created') RETURNING id`, record.ID, second.AttemptNo+1, languageID)
	view = read()
	if view.Result == nil || view.Result.AttemptID != second.ID || view.AdminDiagnostics == nil || view.AdminDiagnostics.ID != second.ID {
		t.Fatalf("read returned a newer non-effective attempt: %+v", view)
	}
	stats, err := queries.GetProblemStats(ctx, problemID)
	if err != nil || stats.TotalSubmissions != 1 || stats.AcceptedSubmissions != 1 {
		t.Fatalf("problem stats read workflow status instead of accepted result: %+v, err=%v", stats, err)
	}
	accepted := StatusAccepted
	views, total, err := reader.ListSubmissions(ctx, actor, ListSubmissionsInput{UserID: &userID, Status: &accepted, Limit: 10})
	if err != nil || total != 1 || len(views) != 1 || views[0].Result == nil || views[0].Result.AttemptID != second.ID {
		t.Fatalf("accepted list did not use effective outcome: %+v, total=%d, err=%v", views, total, err)
	}

}
