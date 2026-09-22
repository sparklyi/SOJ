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

func TestUserRunCounterCleanedUpWhenIdle(t *testing.T) {
	repo := runTestRepo()
	service := newServiceForTest(serviceTestOptions{
		Repository:     repo,
		ProblemReader:  fakeProblemReader{},
		SourceStore:    NewMemorySourceStore(),
		Judge:          judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictAccepted}),
		RunWait:        time.Second,
		RunParallelism: 2,
		RunPerUser:     2,
	})

	out, err := service.CreateRun(t.Context(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRunInput{ProblemID: int64Ptr(1), LanguageID: 71, Source: []byte("package main")})
	if err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	waitForRunStatus(t, repo, out.Run.ID, StatusAccepted)

	service.runs.runMu.Lock()
	tracked := len(service.runs.userRuns)
	service.runs.runMu.Unlock()
	if tracked != 0 {
		t.Fatalf("tracked users after completion=%d, want 0: zero entries must be deleted, not stored", tracked)
	}
	if used := len(service.runs.runSlots); used != 0 {
		t.Fatalf("global slots in use=%d, want 0", used)
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

	// A rejected run must not hold onto a global slot. Checking the per-user gate
	// after the global pool is what leaks here: the slot is taken, the run is
	// refused, and nothing gives the slot back, so other users starve.
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
