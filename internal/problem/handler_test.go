package problem

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"SOJ/internal/auth"
	"SOJ/internal/httpapi"
)

func TestRunProblemCheckReturnsCreatedEnvelope(t *testing.T) {
	startedAt := time.Date(2026, 7, 10, 9, 30, 0, 0, time.UTC)
	checks := &fakeProblemCheckService{
		result: ProblemCheckResult{
			Run: ProblemCheckRun{
				ID:            99,
				ProblemID:     1,
				TestcaseSetID: 7,
				RequestedBy:   10,
				Status:        "completed",
				Summary: ProblemCheckSummary{
					CaseCount:    2,
					FindingCount: 1,
					WarningCount: 1,
					Valid:        false,
				},
				StartedAt:  startedAt,
				FinishedAt: startedAt,
				CreatedAt:  startedAt,
				UpdatedAt:  startedAt,
			},
			Findings: []ProblemCheckFinding{{
				ID:        5,
				RunID:     99,
				Severity:  "warning",
				Code:      "statement.sample_output_empty",
				Message:   "sample output is empty",
				CaseIndex: 1,
				Details:   json.RawMessage(`{"field":"samples[0].output"}`),
				CreatedAt: startedAt,
			}},
		},
	}
	service := &Service{}
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{
		&Module{handler: &Handler{service: service, checkRunner: checks, checkGetter: checks}},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/problems/1/checks", nil)
	req.Header.Set("X-User-ID", "10")
	req.Header.Set("X-User-Role", "user")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if checks.runProblemID != 1 || checks.runActor.UserID != 10 {
		t.Fatalf("RunProblemCheck args problemID=%d actor=%+v", checks.runProblemID, checks.runActor)
	}
	var envelope struct {
		Data  ProblemCheckRun    `json:"data"`
		Error *httpapi.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("unexpected error body: %+v", envelope.Error)
	}
	if envelope.Data.ID != 99 || envelope.Data.Summary.WarningCount != 1 || len(envelope.Data.Findings) != 1 {
		t.Fatalf("unexpected data: %+v", envelope.Data)
	}
}

func TestGetProblemCheckParsesCheckIDAndReturnsEnvelope(t *testing.T) {
	createdAt := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
	checks := &fakeProblemCheckService{
		result: ProblemCheckResult{
			Run: ProblemCheckRun{
				ID:        99,
				ProblemID: 1,
				Status:    "completed",
				Summary:   ProblemCheckSummary{CaseCount: 2, Valid: true},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
			},
			Findings: []ProblemCheckFinding{},
		},
	}
	service := &Service{}
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{
		&Module{handler: &Handler{service: service, checkRunner: checks, checkGetter: checks}},
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/problems/1/checks/99", nil)
	req.Header.Set("X-User-ID", "10")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if checks.getProblemID != 1 || checks.getCheckID != 99 {
		t.Fatalf("GetProblemCheck args problemID=%d checkID=%d", checks.getProblemID, checks.getCheckID)
	}
	var envelope struct {
		Data ProblemCheckRun `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Findings == nil || len(envelope.Data.Findings) != 0 {
		t.Fatalf("findings = %#v, want empty array", envelope.Data.Findings)
	}
}

func TestGetProblemCheckRejectsInvalidCheckID(t *testing.T) {
	checks := &fakeProblemCheckService{}
	service := &Service{}
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{
		&Module{handler: &Handler{service: service, checkRunner: checks, checkGetter: checks}},
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/problems/1/checks/not-a-number", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"problem_check.id_invalid"`) {
		t.Fatalf("body missing problem_check.id_invalid: %s", rec.Body.String())
	}
	if checks.getCheckID != 0 {
		t.Fatalf("GetProblemCheck should not be called for invalid check_id")
	}
}

func TestUploadTestcasesMissingArchiveReturnsTestcaseNotReady(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	service := NewService(repo, &fakeStorage{})
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(service)}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/problems/1/testcase-sets", strings.NewReader("case_count=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-User-ID", "10")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"problem.testcase_not_ready"`) {
		t.Fatalf("body missing problem.testcase_not_ready: %s", rec.Body.String())
	}
}

func TestGetProblemAuthoringStateReturnsOwnerWorkspace(t *testing.T) {
	repo := newFakeRepository()
	seedPublishableProblem(repo)
	repo.checkRuns[1] = ProblemCheckRunRecord{
		ID: 1, ProblemID: 1, StatementID: 3, TestcaseSetID: 7, Status: ProblemCheckStatusCompleted,
		Summary: json.RawMessage(`{"valid":true}`),
	}
	service := NewService(repo, &fakeStorage{})
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(service)}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/problems/1/authoring", nil)
	req.Header.Set("X-User-ID", "10")
	req.Header.Set("X-User-Role", "user")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var envelope struct {
		Data ProblemAuthoringState `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Publishable || envelope.Data.Problem.ID != 1 || envelope.Data.LatestCheck == nil {
		t.Fatalf("unexpected authoring state: %+v", envelope.Data)
	}
}

func TestListProblemsMineScopesRequestToCurrentUser(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Title: "Owned", Status: StatusDraft, Visibility: VisibilityPrivate}
	service := NewService(repo, &fakeStorage{})
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(service)}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/problems?mine=true", nil)
	req.Header.Set("X-User-ID", "10")
	req.Header.Set("X-User-Role", "user")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !repo.lastListFilter.Mine || repo.lastListFilter.OwnerUserID != 10 {
		t.Fatalf("list filter = %+v", repo.lastListFilter)
	}
}

func TestCreateProblemStoresTags(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo, &fakeStorage{})

	created, err := service.CreateProblem(t.Context(), auth.Actor{UserID: 10, Role: auth.RoleUser}, CreateProblemInput{
		Title:         "Two Sum",
		Slug:          "two-sum",
		Difficulty:    DifficultyEasy,
		Visibility:    VisibilityPrivate,
		TimeLimitMS:   1000,
		MemoryLimitKB: 262144,
		Tags:          []string{"Array", "Hash Table"},
	})
	if err != nil {
		t.Fatalf("CreateProblem returned error: %v", err)
	}

	response, err := service.ProblemResponse(t.Context(), created)
	if err != nil {
		t.Fatalf("ProblemResponse returned error: %v", err)
	}
	if len(response.Tags) != 2 || response.Tags[0] != "Array" || response.Tags[1] != "Hash Table" {
		t.Fatalf("tags = %v", response.Tags)
	}
}

type fakeProblemCheckService struct {
	result       ProblemCheckResult
	err          error
	runProblemID int64
	getProblemID int64
	getCheckID   int64
	runActor     auth.Actor
	getActor     auth.Actor
}

func (s *fakeProblemCheckService) RunProblemCheck(ctx context.Context, actor auth.Actor, problemID int64) (ProblemCheckResult, error) {
	s.runActor = actor
	s.runProblemID = problemID
	return s.result, s.err
}

func (s *fakeProblemCheckService) GetProblemCheck(ctx context.Context, actor auth.Actor, problemID int64, checkID int64) (ProblemCheckResult, error) {
	s.getActor = actor
	s.getProblemID = problemID
	s.getCheckID = checkID
	return s.result, s.err
}
