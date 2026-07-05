package submission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"SOJ/internal/auth"
	"SOJ/internal/httpapi"
	"SOJ/internal/judge"
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

	time.Sleep(100 * time.Millisecond)
	completed := repo.runs[out.Run.ID]
	if completed.Status != StatusAccepted || completed.Stdout != "ok\n" {
		t.Fatalf("completed run = %+v", completed)
	}
}

func TestReconcilerResetsStaleJudgeTasks(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	repo := newMemoryRepo()
	repo.tasks[1] = JudgeTaskRecord{ID: 1, SubmissionID: 11, Status: "dispatching"}
	repo.tasks[2] = JudgeTaskRecord{ID: 2, SubmissionID: 12, Status: "running"}
	repo.submissions[12] = SubmissionRecord{ID: 12, Status: StatusRunning}
	reconciler := NewReconciler(repo, &Worker{queue: &memoryQueue{}}, func() time.Time { return now })

	count, err := reconciler.ResetStaleTasks(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("ResetStaleTasks returned error: %v", err)
	}
	if count != 2 || repo.tasks[1].Status != "pending" || repo.tasks[2].Status != "pending" || repo.submissions[12].Status != StatusQueued {
		t.Fatalf("count=%d task1=%+v task2=%+v submission=%+v", count, repo.tasks[1], repo.tasks[2], repo.submissions[12])
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
	acked     []string
	dead      []queue.Message
	deadErr   error
	events    *[]string
}

func (q *memoryQueue) Ensure(ctx context.Context) error { return nil }
func (q *memoryQueue) Publish(ctx context.Context, taskID int64, payload []byte) (string, error) {
	q.published = append(q.published, taskID)
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
