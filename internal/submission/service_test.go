package submission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/httpapi"
	"SOJ/internal/judge"
	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/problem"
	"SOJ/internal/queue"

	"github.com/gin-gonic/gin"
)

func TestCompleteSubmissionSkipsExistingTerminalStatus(t *testing.T) {
	repo := newMemoryRepo()
	repo.submissions[1] = SubmissionRecord{ID: 1, Status: StatusAccepted, Score: 100}
	service := NewService(ServiceOptions{Repository: repo})

	got, err := service.CompleteSubmission(context.Background(), 1, judge.Result{Verdict: judge.VerdictWrongAnswer})
	if err != nil {
		t.Fatalf("CompleteSubmission returned error: %v", err)
	}
	if got.Status != StatusAccepted || repo.submissionUpdates != 0 {
		t.Fatalf("submission = %+v, updates = %d", got, repo.submissionUpdates)
	}
}

func TestCompleteSubmissionPersistsJudgeEvidence(t *testing.T) {
	repo := newMemoryRepo()
	repo.submissions[1] = SubmissionRecord{ID: 1, UserID: 5, ProblemID: 11, LanguageID: 71, TestcaseSetID: 3, Status: StatusRunning}
	service := NewService(ServiceOptions{Repository: repo})
	exitCode := int32(1)

	got, err := service.CompleteSubmission(context.Background(), 1, judge.Result{
		Verdict:       judge.VerdictWrongAnswer,
		TimeMS:        12,
		MemoryKB:      256,
		ErrorMessage:  "first case failed",
		JudgedAt:      time.Unix(200, 0).UTC(),
		CompileOutput: "compile diagnostics",
		Stderr:        "stderr diagnostics",
		Manifest: judge.Manifest{
			JudgeCoreVersion: "core-2026.07",
			JudgeAgentID:     "agent-a",
			LanguageRuntime:  "go1.24",
			SandboxBackend:   "nsjail",
			SandboxProfile:   "default",
			TestcaseSetHash:  "cases-hash",
			CheckerHash:      "checker-hash",
			TraceID:          "trace-1",
		},
		Cases: []judge.CaseResult{
			{Index: 1, TestcaseKey: "cases/1", Verdict: judge.VerdictAccepted, Score: 50, TimeMS: 4, MemoryKB: 128},
			{Index: 2, GroupName: "samples", TestcaseKey: "cases/2", Verdict: judge.VerdictWrongAnswer, Score: 0, TimeMS: 8, MemoryKB: 256, ExitCode: &exitCode, CheckerMessage: "expected 42", OutputDiffSummary: "line 1 differs"},
		},
	})
	if err != nil {
		t.Fatalf("CompleteSubmission returned error: %v", err)
	}
	if got.Status != StatusWrongAnswer || got.Score != 0 || got.TimeMS == nil || *got.TimeMS != 12 {
		t.Fatalf("submission = %+v", got)
	}

	attempt, err := repo.GetLatestJudgeAttemptBySubmissionID(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetLatestJudgeAttemptBySubmissionID returned error: %v", err)
	}
	if attempt.ProtocolVersion != judge.ProtocolVersion || attempt.JudgeCoreVersion != "core-2026.07" || attempt.JudgeEngine != judge.EngineSOJAgent {
		t.Fatalf("attempt protocol fields = %+v", attempt)
	}
	if attempt.Status != StatusWrongAnswer {
		t.Fatalf("attempt status = %q, want %q", attempt.Status, StatusWrongAnswer)
	}
	if attempt.FirstFailedCaseIndex == nil || *attempt.FirstFailedCaseIndex != 2 || attempt.TraceID == nil || *attempt.TraceID != "trace-1" {
		t.Fatalf("attempt failure metadata = %+v", attempt)
	}

	cases, err := repo.ListJudgeCaseResults(context.Background(), attempt.ID)
	if err != nil {
		t.Fatalf("ListJudgeCaseResults returned error: %v", err)
	}
	if len(cases) != 2 || cases[1].Status != StatusWrongAnswer || cases[1].TestcaseKey == nil || *cases[1].TestcaseKey != "cases/2" {
		t.Fatalf("case results = %+v", cases)
	}

	projection, err := repo.GetSubmissionResult(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetSubmissionResult returned error: %v", err)
	}
	if projection.AttemptID != attempt.ID || projection.Status != StatusWrongAnswer || projection.FirstFailedCaseIndex == nil || *projection.FirstFailedCaseIndex != 2 {
		t.Fatalf("projection = %+v attempt = %+v", projection, attempt)
	}
	if !bytes.Contains(projection.SafeSummary, []byte(`"verdict":"wrong_answer"`)) {
		t.Fatalf("safe summary = %s", projection.SafeSummary)
	}
}

func TestWorkerRetriesThenDeadLetters(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	repo.tasks[7] = JudgeTaskRecord{ID: 7, SubmissionID: 9, Status: "dispatched", Attempts: 0}
	repo.submissions[9] = SubmissionRecord{ID: 9, ProblemID: 1, LanguageID: 71, SourceArtifactID: 4, Status: StatusQueued}
	repo.artifacts[4] = ArtifactRecord{ID: 4, StorageKey: "source"}
	repo.languages[71] = LanguageRecord{ID: 71, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144, Enabled: true}
	store := NewMemorySourceStore()
	store.objects["source"] = []byte("bad")
	q := &memoryQueue{}
	engine := judge.NewFakeEngine()
	engine.SetError(errors.New("engine unavailable"))
	worker := NewWorker(WorkerOptions{
		Repository:       repo,
		Queue:            q,
		Judge:            engine,
		ProblemReader:    fakeProblemReader{},
		TestcaseResolver: fakeTestcaseResolver{},
		SourceStore:      store,
		MaxAttempts:      1,
		Now:              func() time.Time { return time.Unix(100, 0).UTC() },
	})

	if err := worker.ProcessMessage(ctx, queue.Message{ID: "1-0", TaskID: 7}); err != nil {
		t.Fatalf("first ProcessMessage returned error: %v", err)
	}
	if repo.tasks[7].Status != "pending" || repo.tasks[7].Attempts != 1 || len(q.acked) != 1 {
		t.Fatalf("after retry task = %+v acked=%v", repo.tasks[7], q.acked)
	}

	if err := worker.ProcessMessage(ctx, queue.Message{ID: "2-0", TaskID: 7}); err != nil {
		t.Fatalf("second ProcessMessage returned error: %v", err)
	}
	if repo.tasks[7].Status != "dead" || len(q.dead) != 1 {
		t.Fatalf("after dead task = %+v dead=%v", repo.tasks[7], q.dead)
	}
}

func TestWorkerDeadLetterOrderAcksOriginalWhenDeadStreamFails(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	repo.tasks[7] = JudgeTaskRecord{ID: 7, SubmissionID: 9, Status: "dispatched", Attempts: 1}
	repo.submissions[9] = SubmissionRecord{ID: 9, ProblemID: 1, LanguageID: 71, SourceArtifactID: 4, Status: StatusQueued}
	repo.artifacts[4] = ArtifactRecord{ID: 4, StorageKey: "source"}
	repo.languages[71] = LanguageRecord{ID: 71, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144, Enabled: true}
	store := NewMemorySourceStore()
	store.objects["source"] = []byte("bad")
	q := &memoryQueue{deadErr: errors.New("dead stream unavailable"), events: &repo.events}
	engine := judge.NewFakeEngine()
	engine.SetError(errors.New("engine unavailable"))
	worker := NewWorker(WorkerOptions{
		Repository:       repo,
		Queue:            q,
		Judge:            engine,
		ProblemReader:    fakeProblemReader{},
		TestcaseResolver: fakeTestcaseResolver{},
		SourceStore:      store,
		MaxAttempts:      1,
	})

	if err := worker.ProcessMessage(ctx, queue.Message{ID: "1-0", TaskID: 7}); err != nil {
		t.Fatalf("ProcessMessage returned error: %v", err)
	}
	want := []string{"db_dead", "dead_stream", "ack"}
	if len(repo.events) != len(want) {
		t.Fatalf("events = %v", repo.events)
	}
	for i := range want {
		if repo.events[i] != want[i] {
			t.Fatalf("events = %v, want %v", repo.events, want)
		}
	}
	if repo.tasks[7].Status != "dead" || len(q.acked) != 1 {
		t.Fatalf("task=%+v acked=%v", repo.tasks[7], q.acked)
	}
}

func TestWorkerRejudgesClaimedRunningTaskWhenSubmissionIsNotTerminal(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	repo.tasks[7] = JudgeTaskRecord{ID: 7, SubmissionID: 9, Status: "running", Attempts: 0}
	repo.submissions[9] = SubmissionRecord{ID: 9, ProblemID: 1, LanguageID: 71, SourceArtifactID: 4, Status: StatusRunning, TestcaseSetID: 3}
	repo.artifacts[4] = ArtifactRecord{ID: 4, StorageKey: "source"}
	repo.languages[71] = LanguageRecord{ID: 71, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144, Enabled: true}
	store := NewMemorySourceStore()
	store.objects["source"] = []byte("package main")
	q := &memoryQueue{}
	engine := judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictAccepted})
	worker := NewWorker(WorkerOptions{
		Repository:       repo,
		Queue:            q,
		Judge:            engine,
		ProblemReader:    fakeProblemReader{},
		TestcaseResolver: fakeTestcaseResolver{},
		SourceStore:      store,
	})

	if err := worker.ProcessMessage(ctx, queue.Message{ID: "1-0", TaskID: 7}); err != nil {
		t.Fatalf("ProcessMessage returned error: %v", err)
	}
	if len(q.acked) != 1 || q.acked[0] != "1-0" {
		t.Fatalf("acked = %v", q.acked)
	}
	if len(engine.Requests()) != 1 {
		t.Fatalf("judge requests = %+v", engine.Requests())
	}
	if repo.tasks[7].Status != "done" || repo.submissions[9].Status != StatusAccepted {
		t.Fatalf("task=%+v submission=%+v", repo.tasks[7], repo.submissions[9])
	}
}

func TestWorkerRecordsJudgeTaskMetrics(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	repo.tasks[7] = JudgeTaskRecord{ID: 7, SubmissionID: 9, Status: "dispatched"}
	repo.submissions[9] = SubmissionRecord{ID: 9, ProblemID: 1, LanguageID: 71, SourceArtifactID: 4, Status: StatusQueued, TestcaseSetID: 3}
	repo.artifacts[4] = ArtifactRecord{ID: 4, StorageKey: "source"}
	repo.languages[71] = LanguageRecord{ID: 71, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144, Enabled: true}
	store := NewMemorySourceStore()
	store.objects["source"] = []byte("package main")
	metrics := &recordingWorkerMetrics{}
	worker := NewWorker(WorkerOptions{
		Repository:       repo,
		Queue:            &memoryQueue{},
		Judge:            judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictAccepted}),
		ProblemReader:    fakeProblemReader{},
		TestcaseResolver: fakeTestcaseResolver{},
		SourceStore:      store,
		Metrics:          metrics,
	})

	if err := worker.ProcessMessage(ctx, queue.Message{ID: "1-0", TaskID: 7}); err != nil {
		t.Fatalf("ProcessMessage returned error: %v", err)
	}
	if len(metrics.processed) != 1 {
		t.Fatalf("processed metrics = %d, want 1", len(metrics.processed))
	}
	if metrics.processed[0].result != "success" || metrics.processed[0].duration <= 0 {
		t.Fatalf("processed metric = %+v", metrics.processed[0])
	}
}

func TestCreateRunJudgesCustomStdinImmediately(t *testing.T) {
	repo := newMemoryRepo()
	repo.languages[71] = LanguageRecord{ID: 71, Enabled: true, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144}
	engine := judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictAccepted, Stdout: "42\n", TimeMS: 12, MemoryKB: 256})
	service := NewService(ServiceOptions{
		Repository:    repo,
		ProblemReader: fakeProblemReader{},
		SourceStore:   NewMemorySourceStore(),
		Judge:         engine,
	})

	out, err := service.CreateRun(context.Background(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: 1, LanguageID: 71, Source: []byte("package main"), Stdin: "21 21\n"})
	if err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	if out.Run.Status != StatusAccepted || out.Run.Stdout != "42\n" {
		t.Fatalf("run = %+v", out.Run)
	}
	requests := engine.Requests()
	if len(requests) != 1 {
		t.Fatalf("judge request count = %d", len(requests))
	}
	if requests[0].Stdin != "21 21\n" || len(requests[0].Testcases) != 0 {
		t.Fatalf("request = %+v", requests[0])
	}
}

func TestListPublicLanguagesReturnsEnabledCatalogForRegularUsers(t *testing.T) {
	repo := newMemoryRepo()
	repo.languages[1] = LanguageRecord{ID: 1, Engine: "soj-agent", EngineLanguageID: "go", Name: "Go", Enabled: true}
	repo.languages[2] = LanguageRecord{ID: 2, Engine: "soj-agent", EngineLanguageID: "cpp17", Name: "C++17", Enabled: false}
	repo.languages[3] = LanguageRecord{ID: 3, Engine: "legacy", EngineLanguageID: "py", Name: "Python", Enabled: true}
	service := NewService(ServiceOptions{Repository: repo})

	engine := "soj-agent"
	items, total, err := service.ListPublicLanguages(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, ListLanguagesInput{Engine: &engine})
	if err != nil {
		t.Fatalf("ListPublicLanguages returned error: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("languages total=%d items=%+v, want one enabled soj-agent language", total, items)
	}
	if items[0].ID != 1 || !items[0].Enabled {
		t.Fatalf("language = %+v", items[0])
	}
}

func TestCreateRunReturnsRunningWhenShortWaitExpiresAndCompletesAsync(t *testing.T) {
	repo := newMemoryRepo()
	repo.languages[71] = LanguageRecord{ID: 71, Enabled: true, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144}
	engine := judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictAccepted, Stdout: "ok\n"})
	engine.SetDelay(50 * time.Millisecond)
	service := NewService(ServiceOptions{
		Repository:    repo,
		ProblemReader: fakeProblemReader{},
		SourceStore:   NewMemorySourceStore(),
		Judge:         engine,
		RunWait:       5 * time.Millisecond,
		RunTimeout:    time.Second,
	})

	out, err := service.CreateRun(context.Background(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: 1, LanguageID: 71, Source: []byte("package main"), Stdin: "21 21\n"})
	if err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	if out.Run.Status != StatusRunning {
		t.Fatalf("run status = %s, want %s", out.Run.Status, StatusRunning)
	}

	completed := waitForRunStatus(t, repo, out.Run.ID, StatusAccepted)
	if completed.Status != StatusAccepted || completed.Stdout != "ok\n" {
		t.Fatalf("completed run = %+v", completed)
	}
}

func TestCreateRunRejectsWhenExecutionCapacityIsExhausted(t *testing.T) {
	repo := newMemoryRepo()
	repo.languages[71] = LanguageRecord{ID: 71, Enabled: true, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144}
	engine := newBlockingRunJudge()
	defer engine.unblock()
	service := NewService(ServiceOptions{
		Repository:    repo,
		ProblemReader: fakeProblemReader{},
		SourceStore:   NewMemorySourceStore(),
		Judge:         engine,
		RunWait:       time.Millisecond,
		RunTimeout:    time.Second,
	})

	first, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: 1, LanguageID: 71, Source: []byte("package main")})
	if err != nil {
		t.Fatalf("first CreateRun returned error: %v", err)
	}
	if first.Run.Status != StatusRunning {
		t.Fatalf("first run status=%s, want %s", first.Run.Status, StatusRunning)
	}
	engine.waitStarted(t)

	_, err = service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: 1, LanguageID: 71, Source: []byte("package main")})
	appErr, ok := apperror.From(err)
	if !ok || appErr.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("second CreateRun error=%v, want service unavailable", err)
	}
	if len(repo.runs) != 1 || len(repo.artifacts) != 1 {
		t.Fatalf("capacity rejection created runs=%d artifacts=%d, want 1/1", len(repo.runs), len(repo.artifacts))
	}

	engine.unblock()
	waitForRunStatus(t, repo, first.Run.ID, StatusAccepted)
}

func TestHandlerCreateRunReturnsServiceUnavailableWhenExecutionCapacityIsExhausted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryRepo()
	repo.languages[71] = LanguageRecord{ID: 71, Enabled: true, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144}
	engine := newBlockingRunJudge()
	defer engine.unblock()
	service := NewService(ServiceOptions{
		Repository:    repo,
		ProblemReader: fakeProblemReader{},
		SourceStore:   NewMemorySourceStore(),
		Judge:         engine,
		RunWait:       time.Millisecond,
		RunTimeout:    time.Second,
	})
	first, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: 1, LanguageID: 71, Source: []byte("package main")})
	if err != nil {
		t.Fatalf("first CreateRun returned error: %v", err)
	}
	engine.waitStarted(t)

	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(NewHandler(service))}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(`{"problem_id":1,"language_id":71,"source_code":"package main"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "5")
	req.Header.Set("X-User-Role", "user")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s, want %d", rec.Code, rec.Body.String(), http.StatusServiceUnavailable)
	}
	if len(repo.runs) != 1 || len(repo.artifacts) != 1 {
		t.Fatalf("capacity rejection created runs=%d artifacts=%d, want 1/1", len(repo.runs), len(repo.artifacts))
	}

	engine.unblock()
	waitForRunStatus(t, repo, first.Run.ID, StatusAccepted)
}

func TestServiceCloseCancelsActiveRunAndRejectsNewRuns(t *testing.T) {
	repo := newMemoryRepo()
	repo.languages[71] = LanguageRecord{ID: 71, Enabled: true, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144}
	engine := newBlockingRunJudge()
	defer engine.unblock()
	service := NewService(ServiceOptions{
		Repository:    repo,
		ProblemReader: fakeProblemReader{},
		SourceStore:   NewMemorySourceStore(),
		Judge:         engine,
		RunWait:       time.Millisecond,
		RunTimeout:    time.Minute,
	})

	first, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: 1, LanguageID: 71, Source: []byte("package main")})
	if err != nil {
		t.Fatalf("first CreateRun returned error: %v", err)
	}
	engine.waitStarted(t)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Close(shutdownCtx); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	completed := waitForRunStatus(t, repo, first.Run.ID, StatusSystemErr)
	if completed.ErrorMessage == nil || *completed.ErrorMessage != context.Canceled.Error() {
		t.Fatalf("completed error message=%v, want %q", completed.ErrorMessage, context.Canceled.Error())
	}
	_, err = service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: 1, LanguageID: 71, Source: []byte("package main")})
	appErr, ok := apperror.From(err)
	if !ok || appErr.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("CreateRun after Close error=%v, want service unavailable", err)
	}
	if len(repo.runs) != 1 || len(repo.artifacts) != 1 {
		t.Fatalf("closed service created runs=%d artifacts=%d, want 1/1", len(repo.runs), len(repo.artifacts))
	}
}

func TestCreateRunRejectsWhenRunContextIsCanceled(t *testing.T) {
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	repo := newMemoryRepo()
	repo.languages[71] = LanguageRecord{ID: 71, Enabled: true, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144}
	service := NewService(ServiceOptions{
		Repository:    repo,
		ProblemReader: fakeProblemReader{},
		SourceStore:   NewMemorySourceStore(),
		Judge:         judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictAccepted}),
		RunContext:    runCtx,
	})

	cancelRun()
	_, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: 1, LanguageID: 71, Source: []byte("package main")})
	appErr, ok := apperror.From(err)
	if !ok || appErr.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("CreateRun after run context cancellation error=%v, want service unavailable", err)
	}
	if len(repo.runs) != 0 || len(repo.artifacts) != 0 {
		t.Fatalf("canceled run context created runs=%d artifacts=%d, want 0/0", len(repo.runs), len(repo.artifacts))
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	if err := service.Close(shutdownCtx); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func waitForRunStatus(t *testing.T, repo *memoryRepo, runID int64, status string) RunRecord {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for {
		run, err := repo.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetRun returned error: %v", err)
		}
		if run.Status == status {
			return run
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %d status = %s, want %s", runID, run.Status, status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type blockingRunJudge struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingRunJudge() *blockingRunJudge {
	return &blockingRunJudge{started: make(chan struct{}, 2), release: make(chan struct{})}
}

func (e *blockingRunJudge) Judge(ctx context.Context, request judge.Request) (judge.Result, error) {
	select {
	case e.started <- struct{}{}:
	default:
	}
	select {
	case <-e.release:
		return judge.Result{Verdict: judge.VerdictAccepted}, nil
	case <-ctx.Done():
		return judge.Result{}, ctx.Err()
	}
}

func (e *blockingRunJudge) Languages(ctx context.Context) ([]judge.Language, error) {
	return nil, nil
}

func (e *blockingRunJudge) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-e.started:
	case <-time.After(time.Second):
		t.Fatal("judge did not start")
	}
}

func (e *blockingRunJudge) unblock() {
	e.once.Do(func() { close(e.release) })
}

func TestReconcilerResetsStaleJudgeTasks(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	repo := newMemoryRepo()
	repo.tasks[1] = JudgeTaskRecord{ID: 1, SubmissionID: 11, Status: "dispatching"}
	repo.tasks[2] = JudgeTaskRecord{ID: 2, SubmissionID: 12, Status: "running"}
	repo.submissions[12] = SubmissionRecord{ID: 12, Status: StatusRunning}
	metrics := &recordingReconcilerMetrics{}
	reconciler := NewReconciler(repo, &Worker{queue: &memoryQueue{}}, func() time.Time { return now }, metrics)

	count, err := reconciler.ResetStaleTasks(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("ResetStaleTasks returned error: %v", err)
	}
	if count != 2 || repo.tasks[1].Status != "pending" || repo.tasks[2].Status != "pending" || repo.submissions[12].Status != StatusQueued {
		t.Fatalf("count=%d task1=%+v task2=%+v submission=%+v", count, repo.tasks[1], repo.tasks[2], repo.submissions[12])
	}
	if !metrics.saw("reset_stale_tasks", "success", 2) {
		t.Fatalf("reconciler metrics = %+v", metrics.records)
	}
}

func TestCreateSubmissionStoresSourceAndCreatesPendingTask(t *testing.T) {
	repo := newMemoryRepo()
	repo.languages[71] = LanguageRecord{ID: 71, Enabled: true}
	service := NewService(ServiceOptions{
		Repository:       repo,
		ProblemReader:    fakeProblemReader{},
		TestcaseResolver: fakeTestcaseResolver{},
		SourceStore:      NewMemorySourceStore(),
		Now:              func() time.Time { return time.Unix(10, 0).UTC() },
	})

	out, err := service.CreateSubmission(context.Background(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateSubmissionInput{ProblemID: 1, LanguageID: 71, Source: []byte("package main")})
	if err != nil {
		t.Fatalf("CreateSubmission returned error: %v", err)
	}
	if out.Submission.Status != StatusQueued || out.Task.Status != "pending" || out.Submission.SourceArtifactID == 0 {
		t.Fatalf("output = %+v", out)
	}
}

func TestCreateSubmissionRollsBackSubmissionWhenTaskCreationFails(t *testing.T) {
	repo := newMemoryRepo()
	repo.languages[71] = LanguageRecord{ID: 71, Enabled: true}
	repo.failCreateJudgeTask = errors.New("task insert failed")
	service := NewService(ServiceOptions{
		Repository:       repo,
		ProblemReader:    fakeProblemReader{},
		TestcaseResolver: fakeTestcaseResolver{},
		SourceStore:      NewMemorySourceStore(),
		Now:              func() time.Time { return time.Unix(10, 0).UTC() },
	})

	_, err := service.CreateSubmission(context.Background(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateSubmissionInput{ProblemID: 1, LanguageID: 71, Source: []byte("package main")})
	if err == nil {
		t.Fatalf("CreateSubmission returned nil error")
	}
	if len(repo.submissions) != 0 || len(repo.tasks) != 0 {
		t.Fatalf("submissions=%v tasks=%v", repo.submissions, repo.tasks)
	}
}

func TestWorkerDefaultRetryPolicyUsesFiveRetriesAndConfiguredBackoff(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	repo := newMemoryRepo()
	repo.tasks[7] = JudgeTaskRecord{ID: 7, SubmissionID: 9, Status: "dispatched", Attempts: 4}
	repo.submissions[9] = SubmissionRecord{ID: 9, ProblemID: 1, LanguageID: 71, SourceArtifactID: 4, Status: StatusQueued, TestcaseSetID: 3}
	repo.artifacts[4] = ArtifactRecord{ID: 4, StorageKey: "source"}
	repo.languages[71] = LanguageRecord{ID: 71, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144, Enabled: true}
	store := NewMemorySourceStore()
	store.objects["source"] = []byte("package main")
	engine := judge.NewFakeEngine()
	engine.SetError(errors.New("engine unavailable"))
	worker := NewWorker(WorkerOptions{
		Repository:       repo,
		Queue:            &memoryQueue{},
		Judge:            engine,
		ProblemReader:    fakeProblemReader{},
		TestcaseResolver: fakeTestcaseResolver{},
		SourceStore:      store,
		Now:              func() time.Time { return now },
	})

	if err := worker.ProcessMessage(context.Background(), queue.Message{ID: "1-0", TaskID: 7}); err != nil {
		t.Fatalf("ProcessMessage returned error: %v", err)
	}
	task := repo.tasks[7]
	if task.Status != "pending" || task.Attempts != 5 || !task.NextRunAt.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("task = %+v", task)
	}
}

func TestWorkerRetryAndDeadLetterSynchronizeSubmissionStatus(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	repo.tasks[7] = JudgeTaskRecord{ID: 7, SubmissionID: 9, Status: "dispatched", Attempts: 0}
	repo.submissions[9] = SubmissionRecord{ID: 9, ProblemID: 1, LanguageID: 71, SourceArtifactID: 4, Status: StatusQueued, TestcaseSetID: 3}
	repo.artifacts[4] = ArtifactRecord{ID: 4, StorageKey: "source"}
	repo.languages[71] = LanguageRecord{ID: 71, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144, Enabled: true}
	store := NewMemorySourceStore()
	store.objects["source"] = []byte("bad")
	engine := judge.NewFakeEngine()
	engine.SetError(errors.New("engine unavailable"))
	worker := NewWorker(WorkerOptions{
		Repository:       repo,
		Queue:            &memoryQueue{},
		Judge:            engine,
		ProblemReader:    fakeProblemReader{},
		TestcaseResolver: fakeTestcaseResolver{},
		SourceStore:      store,
		MaxAttempts:      1,
	})

	if err := worker.ProcessMessage(ctx, queue.Message{ID: "1-0", TaskID: 7}); err != nil {
		t.Fatalf("ProcessMessage returned error: %v", err)
	}
	if repo.tasks[7].Status != "pending" || repo.submissions[9].Status != StatusQueued || repo.submissions[9].ErrorMessage == nil {
		t.Fatalf("retry task=%+v submission=%+v", repo.tasks[7], repo.submissions[9])
	}

	if err := worker.ProcessMessage(ctx, queue.Message{ID: "2-0", TaskID: 7}); err != nil {
		t.Fatalf("ProcessMessage returned error: %v", err)
	}
	if repo.tasks[7].Status != "dead" || repo.submissions[9].Status != StatusSystemErr || repo.submissions[9].ErrorMessage == nil {
		t.Fatalf("dead task=%+v submission=%+v", repo.tasks[7], repo.submissions[9])
	}
}

func TestWorkerUsesSubmissionTestcaseSetSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	repo.tasks[7] = JudgeTaskRecord{ID: 7, SubmissionID: 9, Status: "dispatched"}
	repo.submissions[9] = SubmissionRecord{ID: 9, ProblemID: 1, LanguageID: 71, SourceArtifactID: 4, Status: StatusQueued, TestcaseSetID: 3}
	repo.artifacts[4] = ArtifactRecord{ID: 4, StorageKey: "source"}
	repo.languages[71] = LanguageRecord{ID: 71, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144, Enabled: true}
	store := NewMemorySourceStore()
	store.objects["source"] = []byte("package main")
	resolver := fakeSnapshotTestcaseResolver{
		current: problem.TestcaseSet{ID: 99, ProblemID: 1, Cases: []problem.Testcase{{InputKey: "current.in", OutputKey: "current.out"}}},
		byID: map[int64]problem.TestcaseSet{
			3: {ID: 3, ProblemID: 1, Cases: []problem.Testcase{{InputKey: "snapshot.in", OutputKey: "snapshot.out"}}},
		},
	}
	engine := judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictAccepted})
	worker := NewWorker(WorkerOptions{
		Repository:       repo,
		Queue:            &memoryQueue{},
		Judge:            engine,
		ProblemReader:    fakeProblemReader{},
		TestcaseResolver: resolver,
		SourceStore:      store,
	})

	if err := worker.ProcessMessage(ctx, queue.Message{ID: "1-0", TaskID: 7}); err != nil {
		t.Fatalf("ProcessMessage returned error: %v", err)
	}
	requests := engine.Requests()
	if len(requests) != 1 || len(requests[0].Testcases) != 1 {
		t.Fatalf("requests = %+v", requests)
	}
	if got := requests[0].Testcases[0].InputKey; got != "snapshot.in" {
		t.Fatalf("input key = %q", got)
	}
}

func TestHandlerCreateSubmissionAcceptsSourceCodeField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryRepo()
	repo.languages[71] = LanguageRecord{ID: 71, Enabled: true}
	handler := NewHandler(NewService(ServiceOptions{
		Repository:       repo,
		ProblemReader:    fakeProblemReader{},
		TestcaseResolver: fakeTestcaseResolver{},
		SourceStore:      NewMemorySourceStore(),
	}))
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(handler)}})
	body := bytes.NewBufferString(`{"problem_id":1,"language_id":71,"source_code":"package main"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/submissions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "5")
	req.Header.Set("X-User-Role", "user")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := envelope.Data["id"]; !ok {
		t.Fatalf("response data = %+v", envelope.Data)
	}
	if _, ok := envelope.Data["Submission"]; ok {
		t.Fatalf("response leaked service wrapper: %+v", envelope.Data)
	}
}

func TestHandlerListsSubmissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryRepo()
	repo.submissions[1] = SubmissionRecord{ID: 1, UserID: 5, ProblemID: 11, LanguageID: 71, Status: StatusQueued}
	handler := NewHandler(NewService(ServiceOptions{Repository: repo}))
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(handler)}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/submissions?problem_id=11", nil)
	req.Header.Set("X-User-ID", "5")
	req.Header.Set("X-User-Role", "user")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Items    []SubmissionRecord `json:"items"`
			Total    int64              `json:"total"`
			Page     int32              `json:"page"`
			PageSize int32              `json:"page_size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Total != 1 || len(envelope.Data.Items) != 1 || envelope.Data.Items[0].ID != 1 || envelope.Data.Page != 1 || envelope.Data.PageSize != 20 {
		t.Fatalf("response = %+v", envelope.Data)
	}
}

func TestListSubmissionsBuildsBatchedSummariesWithoutCaseDetails(t *testing.T) {
	repo := newMemoryRepo()
	policy := &batchedSubmissionVisibilityPolicy{}
	seedSubmissionListSummaries(repo, 5, 7)
	service := NewService(ServiceOptions{Repository: repo, ContestPolicy: policy})

	views, total, err := service.ListSubmissions(t.Context(), auth.Actor{UserID: 99, Role: auth.RoleAdmin}, ListSubmissionsInput{Limit: 50})
	if err != nil {
		t.Fatalf("ListSubmissions returned error: %v", err)
	}
	if total != 2 || len(views) != 2 {
		t.Fatalf("ListSubmissions returned total=%d views=%d, want 2", total, len(views))
	}
	for _, view := range views {
		if view.Result == nil {
			t.Fatalf("submission %d is missing its summary result", view.Submission.ID)
		}
		if len(view.Cases) != 0 {
			t.Fatalf("submission %d includes %d case details in a list summary", view.Submission.ID, len(view.Cases))
		}
		if view.AdminDiagnostics == nil {
			t.Fatalf("submission %d is missing admin diagnostics", view.Submission.ID)
		}
	}
	if repo.submissionSummaryLoads != 1 || repo.submissionResultReads != 0 || repo.latestJudgeAttemptReads != 0 || repo.judgeCaseResultReads != 0 {
		t.Fatalf("list detail reads: summaries=%d results=%d attempts=%d cases=%d, want 1/0/0/0", repo.submissionSummaryLoads, repo.submissionResultReads, repo.latestJudgeAttemptReads, repo.judgeCaseResultReads)
	}
	if policy.batchCalls != 1 || policy.singleCalls != 0 {
		t.Fatalf("contest visibility calls: batch=%d single=%d, want 1/0", policy.batchCalls, policy.singleCalls)
	}
}

func TestListOwnSubmissionsByCursorUsesKeysetPagination(t *testing.T) {
	repo := newMemoryRepo()
	newest := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	older := newest.Add(-time.Minute)
	repo.submissions[4] = SubmissionRecord{ID: 4, UserID: 5, ProblemID: 11, LanguageID: 71, Status: StatusQueued, SubmittedAt: newest}
	repo.submissions[3] = SubmissionRecord{ID: 3, UserID: 5, ProblemID: 11, LanguageID: 71, Status: StatusQueued, SubmittedAt: newest}
	repo.submissions[2] = SubmissionRecord{ID: 2, UserID: 5, ProblemID: 11, LanguageID: 71, Status: StatusQueued, SubmittedAt: older}
	repo.submissions[1] = SubmissionRecord{ID: 1, UserID: 6, ProblemID: 11, LanguageID: 71, Status: StatusQueued, SubmittedAt: newest.Add(time.Minute)}
	service := NewService(ServiceOptions{Repository: repo})
	actor := auth.Actor{UserID: 5, Role: auth.RoleUser}

	first, err := service.ListOwnSubmissionsByCursor(t.Context(), actor, ListOwnSubmissionsCursorInput{Limit: 2})
	if err != nil {
		t.Fatalf("ListOwnSubmissionsByCursor first page: %v", err)
	}
	if got := submissionViewIDs(first.Items); !slices.Equal(got, []int64{4, 3}) {
		t.Fatalf("first cursor page IDs=%v, want [4 3]", got)
	}
	if first.NextCursor == nil || !first.NextCursor.SubmittedAt.Equal(newest) || first.NextCursor.ID != 3 {
		t.Fatalf("first next cursor=%+v, want newest submission ID 3", first.NextCursor)
	}
	if repo.listSubmissionCalls != 0 || repo.cursorSubmissionListCalls != 1 {
		t.Fatalf("list calls page=%d cursor=%d, want 0/1", repo.listSubmissionCalls, repo.cursorSubmissionListCalls)
	}

	second, err := service.ListOwnSubmissionsByCursor(t.Context(), actor, ListOwnSubmissionsCursorInput{Cursor: first.NextCursor, Limit: 2})
	if err != nil {
		t.Fatalf("ListOwnSubmissionsByCursor second page: %v", err)
	}
	if got := submissionViewIDs(second.Items); !slices.Equal(got, []int64{2}) {
		t.Fatalf("second cursor page IDs=%v, want [2]", got)
	}
	if second.NextCursor != nil {
		t.Fatalf("second next cursor=%+v, want nil", second.NextCursor)
	}
}

func TestHandlerListsOwnSubmissionsByCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryRepo()
	newest := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	repo.submissions[3] = SubmissionRecord{ID: 3, UserID: 5, ProblemID: 11, LanguageID: 71, Status: StatusQueued, SubmittedAt: newest}
	repo.submissions[2] = SubmissionRecord{ID: 2, UserID: 5, ProblemID: 11, LanguageID: 71, Status: StatusQueued, SubmittedAt: newest.Add(-time.Minute)}
	repo.submissions[1] = SubmissionRecord{ID: 1, UserID: 5, ProblemID: 11, LanguageID: 71, Status: StatusQueued, SubmittedAt: newest.Add(-2 * time.Minute)}
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(NewHandler(NewService(ServiceOptions{Repository: repo})))}})

	firstReq := httptest.NewRequest(http.MethodGet, "/api/v1/submissions/mine?page_size=2", nil)
	firstReq.Header.Set("X-User-ID", "5")
	firstReq.Header.Set("X-User-Role", "user")
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first cursor status=%d body=%s", firstRec.Code, firstRec.Body.String())
	}
	var first struct {
		Data struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
			NextCursor string          `json:"next_cursor"`
			Total      json.RawMessage `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(firstRec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first cursor page: %v", err)
	}
	if len(first.Data.Items) != 2 {
		t.Fatalf("first cursor item count=%d, want 2", len(first.Data.Items))
	}
	if got := []int64{first.Data.Items[0].ID, first.Data.Items[1].ID}; !slices.Equal(got, []int64{3, 2}) {
		t.Fatalf("first cursor page IDs=%v, want [3 2]", got)
	}
	if first.Data.NextCursor == "" {
		t.Fatal("first cursor page missing next_cursor")
	}
	if len(first.Data.Total) != 0 {
		t.Fatalf("cursor page unexpectedly includes total=%s", first.Data.Total)
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/api/v1/submissions/mine?cursor="+first.Data.NextCursor, nil)
	secondReq.Header.Set("X-User-ID", "5")
	secondReq.Header.Set("X-User-Role", "user")
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second cursor status=%d body=%s", secondRec.Code, secondRec.Body.String())
	}
	var second struct {
		Data struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
			NextCursor string `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(secondRec.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second cursor page: %v", err)
	}
	if len(second.Data.Items) != 1 {
		t.Fatalf("second cursor item count=%d, want 1", len(second.Data.Items))
	}
	if got := []int64{second.Data.Items[0].ID}; !slices.Equal(got, []int64{1}) {
		t.Fatalf("second cursor page IDs=%v, want [1]", got)
	}
	if second.Data.NextCursor != "" {
		t.Fatalf("second cursor next=%q, want empty", second.Data.NextCursor)
	}
	if repo.listSubmissionCalls != 0 || repo.cursorSubmissionListCalls != 2 {
		t.Fatalf("list calls page=%d cursor=%d, want 0/2", repo.listSubmissionCalls, repo.cursorSubmissionListCalls)
	}
}

func TestHandlerRejectsInvalidOwnSubmissionCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(NewHandler(NewService(ServiceOptions{Repository: newMemoryRepo()})))}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/submissions/mine?cursor=invalid", nil)
	req.Header.Set("X-User-ID", "5")
	req.Header.Set("X-User-Role", "user")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func submissionViewIDs(views []SubmissionView) []int64 {
	ids := make([]int64, 0, len(views))
	for _, view := range views {
		ids = append(ids, view.Submission.ID)
	}
	return ids
}

func TestHandlerListsSubmissionSummariesWithoutCaseDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryRepo()
	seedSubmissionListSummaries(repo, 5, 7)
	handler := NewHandler(NewService(ServiceOptions{Repository: repo, ContestPolicy: &batchedSubmissionVisibilityPolicy{}}))
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(handler)}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/submissions", nil)
	req.Header.Set("X-User-ID", "5")
	req.Header.Set("X-User-Role", "user")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Items []struct {
				ID     int64           `json:"id"`
				Result json.RawMessage `json:"result"`
				Cases  json.RawMessage `json:"cases"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(envelope.Data.Items) != 2 {
		t.Fatalf("items=%d, want 2", len(envelope.Data.Items))
	}
	for _, item := range envelope.Data.Items {
		if len(item.Result) == 0 || string(item.Result) == "null" {
			t.Fatalf("submission %d is missing its summary result", item.ID)
		}
		if len(item.Cases) != 0 && string(item.Cases) != "null" {
			t.Fatalf("submission %d returned case details: %s", item.ID, item.Cases)
		}
	}
}

func TestListSubmissionsIncludesDiagnosticsAllowedForNonAdmin(t *testing.T) {
	repo := newMemoryRepo()
	policy := &batchedSubmissionVisibilityPolicy{showAdminDiagnostics: true}
	seedSubmissionListSummaries(repo, 5, 7)
	service := NewService(ServiceOptions{Repository: repo, ContestPolicy: policy})

	views, _, err := service.ListSubmissions(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, ListSubmissionsInput{Limit: 50})
	if err != nil {
		t.Fatalf("ListSubmissions returned error: %v", err)
	}
	for _, view := range views {
		if view.AdminDiagnostics == nil {
			t.Fatalf("submission %d is missing diagnostics allowed by its visibility policy", view.Submission.ID)
		}
	}
	if repo.submissionSummaryLoads != 1 || repo.latestJudgeAttemptReads != 0 || repo.judgeCaseResultReads != 0 {
		t.Fatalf("list detail reads: summaries=%d attempts=%d cases=%d, want 1/0/0", repo.submissionSummaryLoads, repo.latestJudgeAttemptReads, repo.judgeCaseResultReads)
	}
}

func seedSubmissionListSummaries(repo *memoryRepo, userID, contestID int64) {
	for _, id := range []int64{1, 2} {
		submissionID := id
		attemptID := id + 100
		repo.submissions[submissionID] = SubmissionRecord{
			ID:          submissionID,
			UserID:      userID,
			ProblemID:   11,
			ContestID:   &contestID,
			LanguageID:  71,
			Status:      StatusAccepted,
			Score:       100,
			SubmittedAt: time.Unix(id, 0).UTC(),
		}
		repo.results[submissionID] = SubmissionResultRecord{SubmissionID: submissionID, AttemptID: attemptID, Status: StatusAccepted, Score: 100}
		repo.attempts[attemptID] = JudgeAttemptRecord{ID: attemptID, SubmissionID: &submissionID, AttemptNo: 1, Status: StatusAccepted}
		repo.cases[attemptID] = []JudgeCaseResultRecord{{ID: attemptID + 100, AttemptID: attemptID, CaseIndex: 1, Status: StatusAccepted, Score: 100}}
	}
}

type batchedSubmissionVisibilityPolicy struct {
	singleCalls          int
	batchCalls           int
	showAdminDiagnostics bool
}

func (p *batchedSubmissionVisibilityPolicy) ValidateSubmission(ctx context.Context, actor auth.Actor, problemID, contestID int64) error {
	return nil
}

func (p *batchedSubmissionVisibilityPolicy) SubmissionResultVisibility(ctx context.Context, actor auth.Actor, sub ContestSubmissionVisibility) (SubmissionResultVisibility, error) {
	p.singleCalls++
	return SubmissionResultVisibility{ShowResult: true, ShowCases: true, ShowAdminDiagnostics: p.showAdminDiagnostics, Visibility: "visible"}, nil
}

func (p *batchedSubmissionVisibilityPolicy) SubmissionResultVisibilities(ctx context.Context, actor auth.Actor, submissions []ContestSubmissionVisibility) (map[int64]SubmissionResultVisibility, error) {
	p.batchCalls++
	visibilities := make(map[int64]SubmissionResultVisibility, len(submissions))
	for _, sub := range submissions {
		visibilities[sub.ID] = SubmissionResultVisibility{ShowResult: true, ShowCases: true, ShowAdminDiagnostics: p.showAdminDiagnostics, Visibility: "visible"}
	}
	return visibilities, nil
}

func TestHandlerSubmissionDetailProjectsSafeResultForOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryRepo()
	repo.submissions[1] = SubmissionRecord{ID: 1, UserID: 5, ProblemID: 11, LanguageID: 71, TestcaseSetID: 3, Status: StatusRunning}
	service := NewService(ServiceOptions{Repository: repo})
	_, err := service.CompleteSubmission(context.Background(), 1, judge.Result{
		Verdict: judge.VerdictWrongAnswer,
		Cases: []judge.CaseResult{
			{Index: 1, TestcaseKey: "hidden/case-1", Verdict: judge.VerdictAccepted, Score: 50},
			{Index: 2, TestcaseKey: "hidden/case-2", Verdict: judge.VerdictWrongAnswer, CheckerMessage: "expected 42", OutputDiffSummary: "line 1 differs"},
		},
	})
	if err != nil {
		t.Fatalf("CompleteSubmission returned error: %v", err)
	}
	handler := NewHandler(service)
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(handler)}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/submissions/1", nil)
	req.Header.Set("X-User-ID", "5")
	req.Header.Set("X-User-Role", "user")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("hidden/case-2")) || bytes.Contains(rec.Body.Bytes(), []byte("admin_diagnostics")) {
		t.Fatalf("response leaked hidden evidence: %s", rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Visibility string `json:"visibility"`
			Result     *struct {
				Status string `json:"status"`
			} `json:"result"`
			Cases []struct {
				CaseIndex      int32   `json:"case_index"`
				CheckerMessage *string `json:"checker_message"`
			} `json:"cases"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Visibility != "visible" || envelope.Data.Result == nil || envelope.Data.Result.Status != StatusWrongAnswer || len(envelope.Data.Cases) != 2 {
		t.Fatalf("response = %+v", envelope.Data)
	}
	if envelope.Data.Cases[1].CheckerMessage == nil || *envelope.Data.Cases[1].CheckerMessage != "expected 42" {
		t.Fatalf("cases = %+v", envelope.Data.Cases)
	}
}

func TestHandlerSubmissionDetailIncludesAdminDiagnosticsForAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryRepo()
	repo.submissions[1] = SubmissionRecord{ID: 1, UserID: 5, ProblemID: 11, LanguageID: 71, TestcaseSetID: 3, Status: StatusRunning}
	service := NewService(ServiceOptions{Repository: repo})
	_, err := service.CompleteSubmission(context.Background(), 1, judge.Result{
		Verdict: judge.VerdictWrongAnswer,
		Manifest: judge.Manifest{
			JudgeCoreVersion: "core-2026.07",
			JudgeAgentID:     "agent-a",
			LanguageRuntime:  "go1.24",
			SandboxBackend:   "nsjail",
			SandboxProfile:   "default",
			TraceID:          "trace-admin",
		},
	})
	if err != nil {
		t.Fatalf("CompleteSubmission returned error: %v", err)
	}
	handler := NewHandler(service)
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(handler)}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/submissions/1", nil)
	req.Header.Set("X-User-ID", "99")
	req.Header.Set("X-User-Role", "admin")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			AdminDiagnostics *struct {
				ProtocolVersion string  `json:"protocol_version"`
				JudgeAgentID    *string `json:"judge_agent_id"`
				TraceID         *string `json:"trace_id"`
			} `json:"admin_diagnostics"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.AdminDiagnostics == nil || envelope.Data.AdminDiagnostics.ProtocolVersion != judge.ProtocolVersion {
		t.Fatalf("admin diagnostics = %+v", envelope.Data.AdminDiagnostics)
	}
	if envelope.Data.AdminDiagnostics.TraceID == nil || *envelope.Data.AdminDiagnostics.TraceID != "trace-admin" {
		t.Fatalf("admin diagnostics trace = %+v", envelope.Data.AdminDiagnostics)
	}
}

func TestHandlerSubmissionDetailIncludesAsyncOTelTraceIDForAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryRepo()
	repo.submissions[1] = SubmissionRecord{ID: 1, UserID: 5, ProblemID: 11, LanguageID: 71, TestcaseSetID: 3, Status: StatusRunning}
	attempt, err := repo.EnsureJudgeAttempt(context.Background(), EnsureJudgeAttemptInput{
		SubmissionID:    1,
		TaskID:          7,
		LanguageID:      71,
		ProtocolVersion: judgeevents.RequestEventType,
		JudgeEngine:     judge.EngineSOJAgent,
		TraceID:         "trace-submission-1-task-7",
	})
	if err != nil {
		t.Fatalf("EnsureJudgeAttempt returned error: %v", err)
	}
	otelTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	payload, err := json.Marshal(judgeevents.ResultEvent{
		EventID:        "evt-result-1",
		RequestEventID: "evt-request-1",
		AttemptID:      strconv.FormatInt(attempt.ID, 10),
		TraceID:        otelTraceID,
		Status:         judge.VerdictAccepted,
		Result:         judge.Result{Verdict: judge.VerdictAccepted, JudgedAt: time.Unix(101, 0).UTC()},
		JudgedAt:       time.Unix(101, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("marshal result event: %v", err)
	}
	consumer := NewResultConsumer(ResultConsumerOptions{Repository: repo})
	if err := consumer.ProcessResultMessage(context.Background(), queue.Message{ID: "2-0", TaskID: 7, Payload: payload}, &memoryQueue{}); err != nil {
		t.Fatalf("ProcessResultMessage returned error: %v", err)
	}

	handler := NewHandler(NewService(ServiceOptions{Repository: repo}))
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(handler)}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/submissions/1", nil)
	req.Header.Set("X-User-ID", "99")
	req.Header.Set("X-User-Role", "admin")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			AdminDiagnostics *struct {
				TraceID *string `json:"trace_id"`
			} `json:"admin_diagnostics"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.AdminDiagnostics == nil || envelope.Data.AdminDiagnostics.TraceID == nil || *envelope.Data.AdminDiagnostics.TraceID != otelTraceID {
		t.Fatalf("admin diagnostics trace = %+v, want %s", envelope.Data.AdminDiagnostics, otelTraceID)
	}
}

func TestFrozenContestSubmissionHidesResultForContestant(t *testing.T) {
	start := time.Unix(1000, 0).UTC()
	freeze := start.Add(time.Hour)
	policy := frozenSubmissionPolicy{freezeAt: freeze, endAt: start.Add(2 * time.Hour), now: freeze.Add(30 * time.Minute)}
	repo := newMemoryRepo()
	contestID := int64(7)
	judgedAt := freeze.Add(2 * time.Minute)
	repo.submissions[1] = SubmissionRecord{ID: 1, UserID: 5, ProblemID: 11, ContestID: &contestID, LanguageID: 71, TestcaseSetID: 3, Status: StatusRunning, SubmittedAt: freeze.Add(time.Minute), JudgedAt: &judgedAt}
	service := NewService(ServiceOptions{Repository: repo, ContestPolicy: policy, Now: func() time.Time { return freeze.Add(30 * time.Minute) }})
	_, err := service.CompleteSubmission(context.Background(), 1, judge.Result{Verdict: judge.VerdictAccepted})
	if err != nil {
		t.Fatalf("CompleteSubmission returned error: %v", err)
	}

	view, err := service.GetSubmission(context.Background(), auth.Actor{UserID: 5, Role: auth.RoleUser}, 1)
	if err != nil {
		t.Fatalf("GetSubmission returned error: %v", err)
	}
	if view.Visibility != "frozen" || view.Result != nil || len(view.Cases) != 0 || view.AdminDiagnostics != nil {
		t.Fatalf("view = %+v", view)
	}
}

func TestQueuedRejudgeSubmissionDoesNotExposePreviousResult(t *testing.T) {
	repo := newMemoryRepo()
	repo.submissions[1] = SubmissionRecord{ID: 1, UserID: 5, ProblemID: 11, Status: StatusQueued}
	repo.results[1] = SubmissionResultRecord{SubmissionID: 1, AttemptID: 9, Status: StatusAccepted}
	repo.attempts[9] = JudgeAttemptRecord{ID: 9, SubmissionID: int64Ptr(1), Status: StatusAccepted}
	service := NewService(ServiceOptions{Repository: repo})

	view, err := service.GetSubmission(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, 1)
	if err != nil {
		t.Fatalf("GetSubmission returned error: %v", err)
	}
	if view.Result != nil || len(view.Cases) != 0 || view.AdminDiagnostics != nil {
		t.Fatalf("queued rejudge exposed previous result: %+v", view)
	}
}

type frozenSubmissionPolicy struct {
	freezeAt time.Time
	endAt    time.Time
	now      time.Time
}

func (p frozenSubmissionPolicy) ValidateSubmission(ctx context.Context, actor auth.Actor, problemID, contestID int64) error {
	return nil
}

func (p frozenSubmissionPolicy) SubmissionResultVisibility(ctx context.Context, actor auth.Actor, sub ContestSubmissionVisibility) (SubmissionResultVisibility, error) {
	if actor.Admin() {
		return SubmissionResultVisibility{ShowResult: true, ShowCases: true, ShowAdminDiagnostics: true, Visibility: "visible"}, nil
	}
	now := p.now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !now.Before(p.freezeAt) && now.Before(p.endAt) && !sub.SubmittedAt.Before(p.freezeAt) {
		return SubmissionResultVisibility{Visibility: "frozen"}, nil
	}
	return SubmissionResultVisibility{ShowResult: true, ShowCases: true, Visibility: "visible"}, nil
}

func TestHandlerSyncLanguagesReturnsAcceptedEmptyForRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryRepo()
	engine := judge.NewFakeEngine()
	engine.SetLanguages([]judge.Language{{ID: 71, Name: "Go", Enabled: true}})
	handler := NewHandler(NewService(ServiceOptions{Repository: repo, Judge: engine}))
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(handler)}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/languages/sync", nil)
	req.Header.Set("X-User-ID", "1")
	req.Header.Set("X-User-Role", "root")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty response body, got %q", rec.Body.String())
	}
	if len(repo.languages) != 1 {
		t.Fatalf("languages = %+v", repo.languages)
	}
}

func TestHandlerCreateRunReturnsOpenAPIShapeAndShortWaitStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryRepo()
	repo.languages[71] = LanguageRecord{ID: 71, Enabled: true, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144}
	engine := judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictAccepted, Stdout: "ok\n"})
	handler := NewHandler(NewService(ServiceOptions{
		Repository:    repo,
		ProblemReader: fakeProblemReader{},
		SourceStore:   NewMemorySourceStore(),
		Judge:         engine,
	}))
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(handler)}})
	body := bytes.NewBufferString(`{"problem_id":1,"language_id":71,"source_code":"package main","stdin":"1 2\n"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "5")
	req.Header.Set("X-User-Role", "user")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data["id"] == nil || envelope.Data["status"] != StatusAccepted || envelope.Data["stdout"] != "ok\n" {
		t.Fatalf("response data = %+v", envelope.Data)
	}
	if _, ok := envelope.Data["run_id"]; ok {
		t.Fatalf("response used legacy run_id shape: %+v", envelope.Data)
	}
}

type fakeProblemReader struct{}

func (fakeProblemReader) GetForJudge(ctx context.Context, problemID int64) (problem.Problem, error) {
	return problem.Problem{ID: problemID}, nil
}

type fakeTestcaseResolver struct{}

func (fakeTestcaseResolver) CurrentReadyTestcaseSet(ctx context.Context, problemID int64) (problem.TestcaseSet, error) {
	return problem.TestcaseSet{ID: 3, ProblemID: problemID, Status: "ready", Cases: []problem.Testcase{{InputKey: "in", OutputKey: "out", TimeLimit: time.Second, MemoryKB: 262144}}}, nil
}

func (fakeTestcaseResolver) ReadyTestcaseSet(ctx context.Context, problemID, testcaseSetID int64) (problem.TestcaseSet, error) {
	return problem.TestcaseSet{ID: testcaseSetID, ProblemID: problemID, Status: "ready", Cases: []problem.Testcase{{InputKey: "in", OutputKey: "out", TimeLimit: time.Second, MemoryKB: 262144}}}, nil
}

type fakeSnapshotTestcaseResolver struct {
	current problem.TestcaseSet
	byID    map[int64]problem.TestcaseSet
}

func (r fakeSnapshotTestcaseResolver) CurrentReadyTestcaseSet(ctx context.Context, problemID int64) (problem.TestcaseSet, error) {
	return r.current, nil
}

func (r fakeSnapshotTestcaseResolver) ReadyTestcaseSet(ctx context.Context, problemID, testcaseSetID int64) (problem.TestcaseSet, error) {
	return r.byID[testcaseSetID], nil
}

type memoryQueue struct {
	published []int64
	payloads  [][]byte
	acked     []string
	dead      []queue.Message
	deadErr   error
	events    *[]string
}

func (q *memoryQueue) Ensure(ctx context.Context) error { return nil }
func (q *memoryQueue) Publish(ctx context.Context, taskID int64, payload []byte) (string, error) {
	q.published = append(q.published, taskID)
	q.payloads = append(q.payloads, append([]byte(nil), payload...))
	return "1-0", nil
}
func (q *memoryQueue) Consume(ctx context.Context, limit int, block time.Duration) ([]queue.Message, error) {
	return nil, nil
}
func (q *memoryQueue) ClaimStale(ctx context.Context, minIdle time.Duration, limit int) ([]queue.Message, error) {
	return nil, nil
}
func (q *memoryQueue) Ack(ctx context.Context, messageID string) error {
	q.acked = append(q.acked, messageID)
	if q.events != nil {
		*q.events = append(*q.events, "ack")
	}
	return nil
}
func (q *memoryQueue) DeadLetter(ctx context.Context, message queue.Message, reason string) error {
	q.dead = append(q.dead, message)
	if q.events != nil {
		*q.events = append(*q.events, "dead_stream")
	}
	return q.deadErr
}
func (q *memoryQueue) Close() error { return nil }

type recordedWorkerProcess struct {
	result   string
	duration time.Duration
}

type recordingWorkerMetrics struct {
	dispatched []string
	processed  []recordedWorkerProcess
}

func (m *recordingWorkerMetrics) RecordJudgeTaskDispatch(result string) {
	m.dispatched = append(m.dispatched, result)
}

func (m *recordingWorkerMetrics) RecordJudgeTaskProcess(result string, duration time.Duration) {
	m.processed = append(m.processed, recordedWorkerProcess{result: result, duration: duration})
}

type recordedReconcilerAction struct {
	action string
	result string
	count  int
}

type recordingReconcilerMetrics struct {
	records []recordedReconcilerAction
}

func (m *recordingReconcilerMetrics) RecordReconcilerAction(action, result string, count int) {
	m.records = append(m.records, recordedReconcilerAction{action: action, result: result, count: count})
}

func (m *recordingReconcilerMetrics) saw(action, result string, count int) bool {
	for _, record := range m.records {
		if record.action == action && record.result == result && record.count == count {
			return true
		}
	}
	return false
}
