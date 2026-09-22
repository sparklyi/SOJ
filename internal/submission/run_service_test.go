package submission

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/httpapi"
	"SOJ/internal/judge"
	"SOJ/internal/problem"
)

// recordingProblemReader remembers every problem it was asked about, so a test
// can prove a playground run never consulted it. "We skip the check" has to be
// observable, otherwise a later refactor can quietly make it unconditional.
type recordingProblemReader struct {
	seen []int64
}

func (r *recordingProblemReader) GetForJudge(_ context.Context, problemID int64) (problem.Problem, error) {
	r.seen = append(r.seen, problemID)
	return problem.Problem{ID: problemID, CurrentTestcaseSetID: 3}, nil
}

// unreadyProblemReader mimics the real reader refusing a problem that is not
// published or has no ready testcase set.
type unreadyProblemReader struct {
	calls int
}

func (r *unreadyProblemReader) GetForJudge(_ context.Context, _ int64) (problem.Problem, error) {
	r.calls++
	return problem.Problem{}, apperror.NotFound("problem.not_ready", "problem is not ready for judge")
}

func runTestRepo() *memoryRepo {
	repo := newMemoryRepo()
	repo.languages[71] = LanguageRecord{ID: 71, Enabled: true, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144}
	return repo
}

func TestCreateRunWithoutProblemSkipsProblemCheck(t *testing.T) {
	repo := runTestRepo()
	problems := &recordingProblemReader{}
	service := newServiceForTest(serviceTestOptions{
		Repository:    repo,
		ProblemReader: problems,
		SourceStore:   NewMemorySourceStore(),
		Judge:         judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictAccepted, Stdout: "42\n"}),
		RunWait:       time.Second,
	})

	out, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{LanguageID: 71, Source: []byte("package main")})
	if err != nil {
		t.Fatalf("playground CreateRun returned error: %v", err)
	}
	if len(problems.seen) != 0 {
		t.Fatalf("playground run consulted the problem reader for %v, want no problem check", problems.seen)
	}
	if out.Run.ProblemID != nil {
		t.Fatalf("playground run problem id=%d, want nil", *out.Run.ProblemID)
	}
	if out.Run.Status != StatusAccepted {
		t.Fatalf("playground run status=%s, want %s", out.Run.Status, StatusAccepted)
	}
}

func TestCreateRunWithProblemStillValidates(t *testing.T) {
	repo := runTestRepo()
	problems := &unreadyProblemReader{}
	service := newServiceForTest(serviceTestOptions{
		Repository:    repo,
		ProblemReader: problems,
		SourceStore:   NewMemorySourceStore(),
		Judge:         judge.NewFakeEngine(),
	})

	_, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: int64Ptr(9), LanguageID: 71, Source: []byte("package main")})
	if err == nil {
		t.Fatal("problem-attached run with an unready problem returned no error")
	}
	if problems.calls != 1 {
		t.Fatalf("problem reader calls=%d, want 1: a run with a problem must still validate it", problems.calls)
	}
	if len(repo.runs) != 0 {
		t.Fatalf("rejected run created %d rows, want 0", len(repo.runs))
	}
}

func TestCreateRunRejectsUserOverLimit(t *testing.T) {
	repo := runTestRepo()
	engine := newBlockingRunJudge()
	defer engine.unblock()
	service := newServiceForTest(serviceTestOptions{
		Repository:     repo,
		ProblemReader:  fakeProblemReader{},
		SourceStore:    NewMemorySourceStore(),
		Judge:          engine,
		RunWait:        time.Millisecond,
		RunTimeout:     time.Minute,
		RunParallelism: 4,
		RunPerUser:     2,
	})

	for i := 0; i < 2; i++ {
		if _, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: int64Ptr(1), LanguageID: 71, Source: []byte("package main")}); err != nil {
			t.Fatalf("run %d returned error: %v", i+1, err)
		}
		engine.waitStarted(t)
	}

	_, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: int64Ptr(1), LanguageID: 71, Source: []byte("package main")})
	appErr, ok := apperror.From(err)
	if !ok || appErr.HTTPStatus != http.StatusTooManyRequests || appErr.Code != "run.user_limit_exceeded" {
		t.Fatalf("third run error=%v, want 429 run.user_limit_exceeded", err)
	}
	if len(repo.runs) != 2 {
		t.Fatalf("rejected run created runs=%d, want 2", len(repo.runs))
	}
}

func TestRunLimitIsPerUser(t *testing.T) {
	repo := runTestRepo()
	engine := newBlockingRunJudge()
	defer engine.unblock()
	service := newServiceForTest(serviceTestOptions{
		Repository:     repo,
		ProblemReader:  fakeProblemReader{},
		SourceStore:    NewMemorySourceStore(),
		Judge:          engine,
		RunWait:        time.Millisecond,
		RunTimeout:     time.Minute,
		RunParallelism: 4,
		RunPerUser:     1,
	})

	if _, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: int64Ptr(1), LanguageID: 71, Source: []byte("package main")}); err != nil {
		t.Fatalf("user 5 first run returned error: %v", err)
	}
	engine.waitStarted(t)

	// User 5 is at its cap. User 6 still has an untouched budget and the global
	// pool has room, so this must be admitted.
	if _, err := service.CreateRun(t.Context(), auth.Actor{UserID: 6, Role: auth.RoleUser}, CreateRunInput{ProblemID: int64Ptr(1), LanguageID: 71, Source: []byte("package main")}); err != nil {
		t.Fatalf("user 6 run returned error: %v", err)
	}
	engine.waitStarted(t)

	if _, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: int64Ptr(1), LanguageID: 71, Source: []byte("package main")}); err == nil {
		t.Fatal("user 5 second run returned no error, want per-user limit")
	}
}

func TestRunSlotReleasedAfterCompletion(t *testing.T) {
	repo := runTestRepo()
	engine := newBlockingRunJudge()
	defer engine.unblock()
	service := newServiceForTest(serviceTestOptions{
		Repository:     repo,
		ProblemReader:  fakeProblemReader{},
		SourceStore:    NewMemorySourceStore(),
		Judge:          engine,
		RunWait:        time.Second,
		RunTimeout:     time.Minute,
		RunParallelism: 4,
		RunPerUser:     1,
	})

	first, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: int64Ptr(1), LanguageID: 71, Source: []byte("package main")})
	if err != nil {
		t.Fatalf("first run returned error: %v", err)
	}
	engine.waitStarted(t)

	engine.unblock()
	waitForRunStatus(t, repo, first.Run.ID, StatusAccepted)

	// If userID were not threaded through releaseExecution, the per-user count
	// would never come back down and this user would be locked out for the
	// lifetime of the process. The symptom would be intermittent, so pin it.
	if _, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: int64Ptr(1), LanguageID: 71, Source: []byte("package main")}); err != nil {
		t.Fatalf("run after completion returned error: %v, want the slot to be released", err)
	}
}

// A queued run is not executed by the API at all: it becomes a judge task and
// the judge-agent runs it. This is the production path, so it is pinned here
// rather than only in an integration test.
func TestQueuedRunEnqueuesTaskInsteadOfExecuting(t *testing.T) {
	repo := runTestRepo()
	problems := &recordingProblemReader{}
	service := newServiceForTest(serviceTestOptions{
		Repository:    repo,
		ProblemReader: problems,
		SourceStore:   NewMemorySourceStore(),
		RunWait:       time.Millisecond,
		RunQueued:     true,
	})

	out, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{LanguageID: 71, Source: []byte("package main"), Stdin: "7 8"})
	if err != nil {
		t.Fatalf("queued CreateRun returned error: %v", err)
	}
	if out.Run.Status != StatusQueued {
		t.Fatalf("queued run status=%s, want %s: nothing has executed yet", out.Run.Status, StatusQueued)
	}
	if len(problems.seen) != 0 {
		t.Fatalf("playground run consulted the problem reader for %v, want no problem check", problems.seen)
	}
	task, ok := repo.runTaskForRun(out.Run.ID)
	if !ok {
		t.Fatalf("queued run %d has no judge task, so nothing would ever execute it", out.Run.ID)
	}
	if task.Status != "pending" {
		t.Fatalf("enqueued task status=%s, want pending", task.Status)
	}
	if out.Run.Stdin != "7 8" {
		t.Fatalf("run stdin=%q, want %q: the agent executes with it", out.Run.Stdin, "7 8")
	}
}

// The per-user cap counts runs the user is still waiting on, not requests in
// progress. In queued mode the request is long gone while the run is still in
// flight, so an in-process counter would measure the wrong thing entirely.
func TestQueuedRunPerUserLimitCountsInFlightRuns(t *testing.T) {
	repo := runTestRepo()
	service := newServiceForTest(serviceTestOptions{
		Repository:    repo,
		ProblemReader: fakeProblemReader{},
		SourceStore:   NewMemorySourceStore(),
		RunWait:       time.Millisecond,
		RunPerUser:    2,
		RunQueued:     true,
	})

	for i := 0; i < 2; i++ {
		if _, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{LanguageID: 71, Source: []byte("package main")}); err != nil {
			t.Fatalf("queued run %d returned error: %v", i+1, err)
		}
	}
	if _, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{LanguageID: 71, Source: []byte("package main")}); err == nil {
		t.Fatal("third queued run returned no error, want per-user limit")
	}
	// The two in-flight runs are still queued, so the limit must lift as soon as
	// they reach a terminal status.
	first := repo.runIDsForUser(5)[0]
	if _, err := repo.UpdateRunStatus(t.Context(), first, judge.Result{Verdict: judge.VerdictAccepted}); err != nil {
		t.Fatalf("complete first run: %v", err)
	}
	if _, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{LanguageID: 71, Source: []byte("package main")}); err != nil {
		t.Fatalf("run after completion returned error: %v, want the in-flight count to drop", err)
	}
}

func TestUserLimitRejectionDoesNotConsumeGlobalSlot(t *testing.T) {
	repo := runTestRepo()
	engine := newBlockingRunJudge()
	defer engine.unblock()
	service := newServiceForTest(serviceTestOptions{
		Repository:     repo,
		ProblemReader:  fakeProblemReader{},
		SourceStore:    NewMemorySourceStore(),
		Judge:          engine,
		RunWait:        time.Millisecond,
		RunTimeout:     time.Minute,
		RunParallelism: 4,
		RunPerUser:     1,
	})

	if _, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: int64Ptr(1), LanguageID: 71, Source: []byte("package main")}); err != nil {
		t.Fatalf("first run returned error: %v", err)
	}
	engine.waitStarted(t)

	if _, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: int64Ptr(1), LanguageID: 71, Source: []byte("package main")}); err == nil {
		t.Fatal("second run returned no error, want per-user limit")
	}

	// A rejected run must not hold onto a global slot. The per-user gate lives in
	// the database now, so it is checked after the slot is taken -- which means
	// only the deferred release stands between a rejection and a leaked slot.
	// Without it other users would starve one slot at a time.
	if used := len(service.runs.runSlots); used != 1 {
		t.Fatalf("global slots in use=%d, want 1: a per-user rejection must not consume a global slot", used)
	}
}

func TestHandlerCreateRunWithoutProblemReturnsNullProblemID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := runTestRepo()
	problems := &recordingProblemReader{}
	service := newServiceForTest(serviceTestOptions{
		Repository:    repo,
		ProblemReader: problems,
		SourceStore:   NewMemorySourceStore(),
		Judge:         judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictAccepted, Stdout: "42\n"}),
		RunWait:       time.Second,
	})
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(NewHandler(service))}})
	rec := postRun(router, `{"language_id":71,"source_code":"package main"}`, "5")

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want %d", rec.Code, rec.Body.String(), http.StatusOK)
	}
	var body struct {
		Data struct {
			ProblemID *int64 `json:"problem_id"`
			Status    string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.ProblemID != nil {
		t.Fatalf("response problem_id=%d, want null", *body.Data.ProblemID)
	}
	if len(problems.seen) != 0 {
		t.Fatalf("playground run consulted the problem reader for %v, want no problem check", problems.seen)
	}
}

func TestHandlerCreateRunReturnsTooManyRequestsOverUserLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := runTestRepo()
	engine := newBlockingRunJudge()
	defer engine.unblock()
	service := newServiceForTest(serviceTestOptions{
		Repository:     repo,
		ProblemReader:  fakeProblemReader{},
		SourceStore:    NewMemorySourceStore(),
		Judge:          engine,
		RunWait:        time.Millisecond,
		RunTimeout:     time.Minute,
		RunParallelism: 4,
		RunPerUser:     1,
	})
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(NewHandler(service))}})

	if rec := postRun(router, `{"language_id":71,"source_code":"package main"}`, "5"); rec.Code != http.StatusAccepted {
		t.Fatalf("first run status=%d body=%s, want %d", rec.Code, rec.Body.String(), http.StatusAccepted)
	}
	engine.waitStarted(t)

	rec := postRun(router, `{"language_id":71,"source_code":"package main"}`, "5")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second run status=%d body=%s, want %d", rec.Code, rec.Body.String(), http.StatusTooManyRequests)
	}
	if !strings.Contains(rec.Body.String(), "run.user_limit_exceeded") {
		t.Fatalf("second run body=%s, want run.user_limit_exceeded", rec.Body.String())
	}
}

func postRun(router http.Handler, body, userID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-User-Role", "user")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// The per-user cap has to be a property of the row, not of a request: it is the
// only way it can hold across API replicas, where an in-process counter would
// silently be N times the configured value.
func TestAdmitRunEnforcesLimitAcrossServiceInstances(t *testing.T) {
	repo := runTestRepo()
	options := serviceTestOptions{
		Repository:    repo,
		ProblemReader: fakeProblemReader{},
		SourceStore:   NewMemorySourceStore(),
		RunWait:       time.Millisecond,
		RunPerUser:    1,
		RunQueued:     true,
	}
	// Two services, one database: this is what two API replicas look like.
	first := newServiceForTest(options)
	second := newServiceForTest(options)

	if _, err := first.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{LanguageID: 71, Source: []byte("package main")}); err != nil {
		t.Fatalf("first replica CreateRun returned error: %v", err)
	}
	_, err := second.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{LanguageID: 71, Source: []byte("package main")})
	if err == nil {
		t.Fatal("second replica admitted a run the first replica had already filled the budget with")
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("second replica error=%v, want 429", err)
	}
}

// stdin is stored in the run row and placed on the request event, so it has to be
// bounded. Without a bound one request can bloat both.
func TestCreateRunRejectsOversizedStdin(t *testing.T) {
	repo := runTestRepo()
	service := newServiceForTest(serviceTestOptions{
		Repository:    repo,
		ProblemReader: fakeProblemReader{},
		SourceStore:   NewMemorySourceStore(),
		RunQueued:     true,
	})

	_, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{
		LanguageID: 71,
		Source:     []byte("package main"),
		Stdin:      strings.Repeat("x", defaultRunStdinMaxBytes+1),
	})
	appErr, ok := apperror.From(err)
	if !ok || appErr.HTTPStatus != http.StatusUnprocessableEntity || appErr.Code != "run.stdin_too_large" {
		t.Fatalf("oversized stdin error=%v, want 422 run.stdin_too_large", err)
	}
	if len(repo.runs) != 0 {
		t.Fatalf("rejected run created %d rows, want 0", len(repo.runs))
	}
}
