package submission

import (
	"bytes"
	"context"
	"os"
	"strconv"
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

func TestEffectiveSubmissionOutcomeAcrossRejudge(t *testing.T) {
	dsn := os.Getenv("SOJ_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("SOJ_TEST_DATABASE_DSN is not set")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	queryID := func(sql string, args ...any) int64 {
		t.Helper()
		var id int64
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	token := "outcome-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := queryID(`INSERT INTO users (email, password_hash, username) VALUES ($1, 'hash', $2) RETURNING id`, token+"@example.test", token)
	problemID := queryID(`INSERT INTO problems (owner_user_id, title, slug, difficulty, visibility, time_limit_ms, memory_limit_kb)
		VALUES ($1, 'Outcome test', $2, 'easy', 'public', 1000, 262144) RETURNING id`, userID, token)
	languageID := queryID(`INSERT INTO languages (engine, engine_language_id, name, default_time_limit_ms, default_memory_limit_kb)
		VALUES ('soj-agent', $1, 'Outcome test', 1000, 262144) RETURNING id`, token)
	setID := queryID(`INSERT INTO testcase_sets (problem_id, version, storage_key, checksum_sha256, size_bytes, case_count, created_by)
		VALUES ($1, 1, $2, repeat('a', 64), 1, 1, $3) RETURNING id`, problemID, token, userID)
	artifactID := queryID(`INSERT INTO artifacts (owner_type, owner_id, kind, storage_key, checksum_sha256, size_bytes, content_type)
		VALUES ('submission', $1, 'source', $2, repeat('b', 64), 1, 'text/plain') RETURNING id`, userID, token+"/source")
	queries := db.New(tx)
	repo := NewSQLRepositoryWithTxRunner(queries, outcomeSavepointRunner{tx})
	now := time.Unix(1770000000, 0).UTC()
	record, task, err := repo.CreateSubmissionWithTask(ctx, SubmissionRecord{
		UserID: userID, ProblemID: problemID, LanguageID: languageID, TestcaseSetID: setID, Status: StatusQueued, SourceArtifactID: artifactID,
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
	_ = queryID(`INSERT INTO judge_attempts (submission_id, attempt_no, protocol_version, judge_core_version, judge_engine, language_id, status)
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
