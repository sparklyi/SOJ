package problem

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"SOJ/internal/auth"
	"SOJ/internal/httpapi"
)

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
