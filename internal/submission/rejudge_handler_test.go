package submission

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"SOJ/internal/httpapi"
)

func TestRejudgeHandlerCreateDetailAndCancel(t *testing.T) {
	repo := &fakeRejudgeRepository{
		created: RejudgeBatchRecord{ID: 7, ProblemID: int64Ptr(11), RequestedBy: 5, Status: RejudgeBatchStatusQueued, TotalCount: 2},
		batch:   RejudgeBatchRecord{ID: 7, ProblemID: int64Ptr(11), RequestedBy: 5, Status: RejudgeBatchStatusRunning, TotalCount: 2},
	}
	rejudge := NewRejudgeService(repo, &fakeRejudgePolicy{}, time.Now)
	router := httpapi.NewRouter(httpapi.RouterOptions{Modules: []httpapi.Module{NewModule(NewHandler(NewService(ServiceOptions{}), rejudge))}})

	create := httptest.NewRequest(http.MethodPost, "/api/v1/rejudge-batches", bytes.NewBufferString(`{"problem_id":11,"reason":"fixed testcase"}`))
	create.Header.Set("Content-Type", "application/json")
	create.Header.Set("X-User-ID", "5")
	create.Header.Set("X-User-Role", "user")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, create)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	assertEnvelopeID(t, createRec.Body.Bytes(), 7)

	detail := httptest.NewRequest(http.MethodGet, "/api/v1/rejudge-batches/7", nil)
	detail.Header.Set("X-User-ID", "5")
	detail.Header.Set("X-User-Role", "user")
	detailRec := httptest.NewRecorder()
	router.ServeHTTP(detailRec, detail)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detailRec.Code, detailRec.Body.String())
	}

	cancel := httptest.NewRequest(http.MethodPost, "/api/v1/rejudge-batches/7/cancel", bytes.NewBufferString(`{"reason":"operator canceled"}`))
	cancel.Header.Set("Content-Type", "application/json")
	cancel.Header.Set("X-User-ID", "5")
	cancel.Header.Set("X-User-Role", "user")
	cancelRec := httptest.NewRecorder()
	router.ServeHTTP(cancelRec, cancel)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", cancelRec.Code, cancelRec.Body.String())
	}
}

func assertEnvelopeID(t *testing.T, body []byte, want int64) {
	t.Helper()
	var envelope struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.ID != want {
		t.Fatalf("id=%d want=%d body=%s", envelope.Data.ID, want, string(body))
	}
}
