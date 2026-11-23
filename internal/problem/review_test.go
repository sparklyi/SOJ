package problem

import (
	"context"
	"math"
	"net/http"
	"testing"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

func TestProblemReviewSubmitTransitionsDraftAndRecordsEvent(t *testing.T) {
	store := newReviewMemoryStore(ProblemRecord{ID: 1, OwnerUserID: 7, Status: StatusDraft})
	service := NewProblemReviewService(store, store, allowProblemReviewPolicy{})

	updated, err := service.Submit(context.Background(), auth.Actor{UserID: 7}, 1)
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}
	if updated.Status != StatusInReview {
		t.Fatalf("status = %q, want %q", updated.Status, StatusInReview)
	}
	if len(store.events) != 1 {
		t.Fatalf("events = %d, want 1", len(store.events))
	}
	if event := store.events[0]; event.FromStatus != StatusDraft || event.ToStatus != StatusInReview || event.Decision != "submit" {
		t.Fatalf("event = %#v", event)
	}
}

func TestProblemReviewApproveRequiresReadinessAndPublishesAtomically(t *testing.T) {
	store := newReviewMemoryStore(ProblemRecord{ID: 1, OwnerUserID: 7, Status: StatusInReview})
	store.statement = Statement{ID: 11, ProblemID: 1}
	store.testcaseSet = TestcaseSetRecord{ID: 12, ProblemID: 1}
	store.check = ProblemCheckRunRecord{ID: 13, ProblemID: 1, StatementID: 11, TestcaseSetID: 12, Status: ProblemCheckStatusCompleted, Summary: []byte(`{"valid":true}`)}
	service := NewProblemReviewService(store, store, allowProblemReviewPolicy{})

	updated, err := service.Decide(context.Background(), auth.Actor{UserID: 9}, 1, ProblemReviewDecisionInput{Decision: ReviewDecisionApprove})
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if updated.Status != StatusPublished {
		t.Fatalf("status = %q, want %q", updated.Status, StatusPublished)
	}
	if len(store.events) != 1 || store.events[0].ActorUserID != 9 {
		t.Fatalf("events = %#v", store.events)
	}
}

func TestProblemReviewRequestChangesRequiresComment(t *testing.T) {
	store := newReviewMemoryStore(ProblemRecord{ID: 1, OwnerUserID: 7, Status: StatusInReview})
	service := NewProblemReviewService(store, store, allowProblemReviewPolicy{})

	_, err := service.Decide(context.Background(), auth.Actor{UserID: 9}, 1, ProblemReviewDecisionInput{Decision: ReviewDecisionRequestChanges})
	if err == nil {
		t.Fatal("Decide returned nil error")
	}
	if len(store.events) != 0 || store.problem.Status != StatusInReview {
		t.Fatalf("review mutated on invalid request: problem=%#v events=%#v", store.problem, store.events)
	}
}

func TestProblemReviewRejectsConcurrentStateChange(t *testing.T) {
	store := newReviewMemoryStore(ProblemRecord{ID: 1, OwnerUserID: 7, Status: StatusInReview})
	store.mutateOnLock = func() { store.problem.Status = StatusChangesRequested }
	service := NewProblemReviewService(store, store, allowProblemReviewPolicy{})

	_, err := service.Decide(context.Background(), auth.Actor{UserID: 9}, 1, ProblemReviewDecisionInput{
		Decision: ReviewDecisionRequestChanges,
		Comment:  "add an explanation",
	})
	if err == nil {
		t.Fatal("Decide returned nil error")
	}
	if len(store.events) != 0 {
		t.Fatalf("events = %#v, want none", store.events)
	}
}

func TestProblemReviewQueueOffsetRange(t *testing.T) {
	const pageSize int32 = 20
	const lastPage = math.MaxInt32/pageSize + 1
	lastFilter := ProblemReviewQueueFilter{Page: lastPage, PageSize: pageSize}
	tests := []struct {
		name    string
		filter  ProblemReviewQueueFilter
		want    ProblemReviewQueueFilter
		wantErr bool
	}{
		{name: "last valid page", filter: lastFilter, want: lastFilter},
		{name: "offset overflow", filter: ProblemReviewQueueFilter{Page: lastPage + 1, PageSize: pageSize}, wantErr: true},
		{name: "defaults", want: ProblemReviewQueueFilter{Page: 1, PageSize: 20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newReviewMemoryStore(ProblemRecord{ID: 1, Status: StatusInReview})
			service := NewProblemReviewService(store, store, allowProblemReviewPolicy{})
			queue, err := service.Queue(t.Context(), auth.Actor{UserID: 9}, tt.filter)
			if tt.wantErr {
				appErr, ok := apperror.From(err)
				if !ok || appErr.HTTPStatus != http.StatusBadRequest || appErr.Code != "request.invalid" {
					t.Errorf("Queue error = %v, want request.invalid (HTTP 400)", err)
				}
				if len(store.queueFilters) != 0 {
					t.Errorf("store received filters %v, want no calls", store.queueFilters)
				}
				return
			}
			if err != nil {
				t.Fatalf("Queue returned error: %v", err)
			}
			if len(store.queueFilters) != 1 || store.queueFilters[0] != tt.want {
				t.Errorf("store received filters %v, want [%+v]", store.queueFilters, tt.want)
			}
			if queue.Page != tt.want.Page || queue.PageSize != tt.want.PageSize {
				t.Errorf("queue page = %d, size = %d, want page %d and size %d", queue.Page, queue.PageSize, tt.want.Page, tt.want.PageSize)
			}
		})
	}
}

type allowProblemReviewPolicy struct{}

func (allowProblemReviewPolicy) CanSubmitReview(auth.Actor, ProblemRecord) error { return nil }
func (allowProblemReviewPolicy) CanDecideReview(auth.Actor, ProblemRecord) error { return nil }
func (allowProblemReviewPolicy) CanViewReviewQueue(auth.Actor) error             { return nil }
func (allowProblemReviewPolicy) CanViewReviewEvents(auth.Actor, ProblemRecord) error {
	return nil
}

type reviewMemoryStore struct {
	problem      ProblemRecord
	events       []ProblemReviewEvent
	queueFilters []ProblemReviewQueueFilter
	statement    Statement
	testcaseSet  TestcaseSetRecord
	check        ProblemCheckRunRecord
	mutateOnLock func()
}

func newReviewMemoryStore(problem ProblemRecord) *reviewMemoryStore {
	return &reviewMemoryStore{problem: problem, events: []ProblemReviewEvent{}}
}

func (s *reviewMemoryStore) GetProblem(context.Context, int64) (ProblemRecord, error) {
	if s.problem.ID == 0 {
		return ProblemRecord{}, apperror.NotFound("problem.not_found", "problem not found")
	}
	return s.problem, nil
}

func (s *reviewMemoryStore) ListProblemsForReview(_ context.Context, filter ProblemReviewQueueFilter) ([]ProblemRecord, int64, error) {
	s.queueFilters = append(s.queueFilters, filter)
	if s.problem.Status != StatusInReview {
		return []ProblemRecord{}, 0, nil
	}
	return []ProblemRecord{s.problem}, 1, nil
}

func (s *reviewMemoryStore) ListProblemReviewEvents(context.Context, int64) ([]ProblemReviewEvent, error) {
	return append([]ProblemReviewEvent(nil), s.events...), nil
}

func (s *reviewMemoryStore) WithProblemReviewTx(ctx context.Context, fn func(context.Context, problemReviewTx) error) error {
	return fn(ctx, s)
}

func (s *reviewMemoryStore) LockProblemForUpdate(context.Context, int64) (ProblemRecord, error) {
	if s.mutateOnLock != nil {
		s.mutateOnLock()
		s.mutateOnLock = nil
	}
	return s.problem, nil
}

func (s *reviewMemoryStore) UpdateProblem(_ context.Context, _ int64, input UpdateProblemInput) (ProblemRecord, error) {
	if input.Status != nil {
		s.problem.Status = *input.Status
	}
	return s.problem, nil
}

func (s *reviewMemoryStore) SetProblemStatus(_ context.Context, _ int64, status string) (ProblemRecord, error) {
	s.problem.Status = status
	return s.problem, nil
}

func (s *reviewMemoryStore) CreateProblemReviewEvent(_ context.Context, event ProblemReviewEvent) (ProblemReviewEvent, error) {
	event.ID = int64(len(s.events) + 1)
	s.events = append(s.events, event)
	return event, nil
}

func (s *reviewMemoryStore) GetCurrentProblemStatement(context.Context, int64) (Statement, error) {
	if s.statement.ID == 0 {
		return Statement{}, apperror.NotFound("statement.not_found", "statement not found")
	}
	return s.statement, nil
}

func (s *reviewMemoryStore) GetCurrentTestcaseSet(context.Context, int64) (TestcaseSetRecord, error) {
	if s.testcaseSet.ID == 0 {
		return TestcaseSetRecord{}, apperror.NotFound("testcase_set.not_found", "testcase set not found")
	}
	return s.testcaseSet, nil
}

func (s *reviewMemoryStore) GetLatestCompletedProblemCheckRun(context.Context, int64, int64, int64) (ProblemCheckRunRecord, error) {
	if s.check.ID == 0 {
		return ProblemCheckRunRecord{}, apperror.NotFound("problem_check.not_found", "problem check not found")
	}
	return s.check, nil
}

func (s *reviewMemoryStore) ListProblemCheckFindings(context.Context, int64) ([]ProblemCheckFindingRecord, error) {
	return []ProblemCheckFindingRecord{}, nil
}
