package submission

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// retentionEventLog records the order in which the sweeper touches storage and
// the database. The order is the invariant under test, so it has to be
// observable rather than implied.
type retentionEventLog struct {
	events []string
}

type retentionStoreStub struct {
	log      *retentionEventLog
	expired  []ExpiredRunRecord
	listed   []ListExpiredRunsInput
	deleted  []DeleteExpiredRunInput
	listErr  error
	deleteFn func(DeleteExpiredRunInput) error

	orphans     []OrphanedArtifactRecord
	orphanErr   error
	deletedArts []int64
}

func (s *retentionStoreStub) ListExpiredRuns(_ context.Context, input ListExpiredRunsInput) ([]ExpiredRunRecord, error) {
	s.listed = append(s.listed, input)
	if s.listErr != nil {
		return nil, s.listErr
	}
	if input.Limit > 0 && int(input.Limit) < len(s.expired) {
		return append([]ExpiredRunRecord(nil), s.expired[:input.Limit]...), nil
	}
	return append([]ExpiredRunRecord(nil), s.expired...), nil
}

func (s *retentionStoreStub) DeleteExpiredRun(_ context.Context, input DeleteExpiredRunInput) error {
	s.deleted = append(s.deleted, input)
	if s.deleteFn != nil {
		if err := s.deleteFn(input); err != nil {
			return err
		}
	}
	s.log.events = append(s.log.events, "delete_row")
	return nil
}

func (s *retentionStoreStub) ListOrphanedRunArtifacts(_ context.Context, input ListOrphanedArtifactsInput) ([]OrphanedArtifactRecord, error) {
	if s.orphanErr != nil {
		return nil, s.orphanErr
	}
	if input.Limit > 0 && int(input.Limit) < len(s.orphans) {
		return append([]OrphanedArtifactRecord(nil), s.orphans[:input.Limit]...), nil
	}
	return append([]OrphanedArtifactRecord(nil), s.orphans...), nil
}

func (s *retentionStoreStub) DeleteArtifact(_ context.Context, id int64) error {
	s.deletedArts = append(s.deletedArts, id)
	s.log.events = append(s.log.events, "delete_artifact")
	return nil
}

type retentionObjectStub struct {
	log      *retentionEventLog
	deleted  []string
	deleteFn func(string) error
}

func (s *retentionObjectStub) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	if s.deleteFn != nil {
		if err := s.deleteFn(key); err != nil {
			return err
		}
	}
	s.log.events = append(s.log.events, "delete_object")
	return nil
}

func newRetentionForTest(t *testing.T, age time.Duration, runs ...ExpiredRunRecord) (*RunRetention, *retentionStoreStub, *retentionObjectStub, *retentionEventLog) {
	t.Helper()
	log := &retentionEventLog{}
	store := &retentionStoreStub{log: log, expired: runs}
	objects := &retentionObjectStub{log: log}
	retention := NewRunRetention(RunRetentionOptions{
		Store:   store,
		Objects: objects,
		Now:     func() time.Time { return time.Unix(1_000_000, 0).UTC() },
		Age:     age,
	})
	return retention, store, objects, log
}

// The order is the whole point: the row has to go last. Dropping the row first
// would leave the object behind with nothing pointing at it, and no sweep could
// ever find it again.
func TestRunRetentionDeletesObjectBeforeRow(t *testing.T) {
	retention, store, objects, log := newRetentionForTest(t, 7*24*time.Hour,
		ExpiredRunRecord{ID: 1, SourceArtifactID: int64Ptr(4), StorageKey: "run/1/abc"})

	result, err := retention.Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep returned error: %v", err)
	}
	if result.Deleted != 1 || result.Failed != 0 {
		t.Fatalf("result = %+v, want one deletion and no failures", result)
	}
	if len(log.events) != 2 || log.events[0] != "delete_object" || log.events[1] != "delete_row" {
		t.Fatalf("events = %v, want the object removed before the row", log.events)
	}
	if len(objects.deleted) != 1 || objects.deleted[0] != "run/1/abc" {
		t.Fatalf("objects deleted = %v", objects.deleted)
	}
	if len(store.deleted) != 1 || store.deleted[0].RunID != 1 || store.deleted[0].ArtifactID == nil || *store.deleted[0].ArtifactID != 4 {
		t.Fatalf("rows deleted = %+v", store.deleted)
	}
}

func TestRunRetentionAsksForRunsOlderThanTheWindow(t *testing.T) {
	retention, store, _, _ := newRetentionForTest(t, 48*time.Hour)

	if _, err := retention.Sweep(t.Context()); err != nil {
		t.Fatalf("Sweep returned error: %v", err)
	}
	if len(store.listed) != 1 {
		t.Fatalf("list calls = %d, want 1", len(store.listed))
	}
	cutoff := store.listed[0].CreatedBefore
	want := time.Unix(1_000_000, 0).UTC().Add(-48 * time.Hour)
	if !cutoff.Equal(want) {
		t.Fatalf("created_before = %s, want %s", cutoff, want)
	}
}

// A run whose artifact is already gone still has a row that has to be removed.
// Skipping it would leak the row forever, which is the failure a retention job
// is least likely to be noticed for.
func TestRunRetentionRemovesRowsWithNoArtifactLeft(t *testing.T) {
	retention, store, objects, _ := newRetentionForTest(t, 24*time.Hour,
		ExpiredRunRecord{ID: 9})

	result, err := retention.Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep returned error: %v", err)
	}
	if result.Deleted != 1 {
		t.Fatalf("result = %+v, want the row removed", result)
	}
	if len(objects.deleted) != 0 {
		t.Fatalf("objects deleted = %v, want none: there is no storage key", objects.deleted)
	}
	if len(store.deleted) != 1 || store.deleted[0].ArtifactID != nil {
		t.Fatalf("rows deleted = %+v, want a nil artifact id", store.deleted)
	}
}

// A zero window has to mean "off". If it meant "older than now" the sweep would
// delete every finished run the first time it ran.
func TestRunRetentionDisabledByZeroAge(t *testing.T) {
	retention, store, objects, _ := newRetentionForTest(t, 0,
		ExpiredRunRecord{ID: 1, SourceArtifactID: int64Ptr(4), StorageKey: "run/1/abc"})

	if retention.Enabled() {
		t.Fatal("Enabled() = true for a zero age")
	}
	result, err := retention.Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep returned error: %v", err)
	}
	if result != (RunRetentionResult{}) {
		t.Fatalf("result = %+v, want nothing done", result)
	}
	if len(store.listed) != 0 || len(store.deleted) != 0 || len(objects.deleted) != 0 {
		t.Fatalf("disabled sweep touched storage: listed=%d rows=%d objects=%d", len(store.listed), len(store.deleted), len(objects.deleted))
	}
}

// One unreadable object must not stop the rest of the batch. The failed run has
// to be left completely alone so the next sweep can retry it -- a half-applied
// deletion would be worse than none.
func TestRunRetentionContinuesAfterOneFailure(t *testing.T) {
	retention, store, objects, _ := newRetentionForTest(t, 24*time.Hour,
		ExpiredRunRecord{ID: 1, SourceArtifactID: int64Ptr(4), StorageKey: "run/1/abc"},
		ExpiredRunRecord{ID: 2, SourceArtifactID: int64Ptr(5), StorageKey: "run/2/def"},
		ExpiredRunRecord{ID: 3, SourceArtifactID: int64Ptr(6), StorageKey: "run/3/ghi"},
	)
	objects.deleteFn = func(key string) error {
		if key == "run/2/def" {
			return errors.New("storage unavailable")
		}
		return nil
	}

	result, err := retention.Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep returned error: %v", err)
	}
	if result.Scanned != 3 || result.Deleted != 2 || result.Failed != 1 {
		t.Fatalf("result = %+v, want 2 deleted and 1 failed out of 3", result)
	}
	for _, deleted := range store.deleted {
		if deleted.RunID == 2 {
			t.Fatalf("run 2 was deleted even though its object could not be removed: %+v", store.deleted)
		}
	}
}

// A sweep removes one batch. The rest waits for the next one, so a large backlog
// never turns into a single long transaction.
func TestRunRetentionSweepIsBoundedByBatch(t *testing.T) {
	log := &retentionEventLog{}
	store := &retentionStoreStub{log: log}
	for id := int64(1); id <= 5; id++ {
		store.expired = append(store.expired, ExpiredRunRecord{ID: id})
	}
	retention := NewRunRetention(RunRetentionOptions{
		Store:   store,
		Objects: &retentionObjectStub{log: log},
		Age:     time.Hour,
		Batch:   2,
	})

	result, err := retention.Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep returned error: %v", err)
	}
	if result.Deleted != 2 || len(store.deleted) != 2 {
		t.Fatalf("result = %+v deleted=%d, want the batch limit honoured", result, len(store.deleted))
	}
	if store.listed[0].Limit != 2 {
		t.Fatalf("limit = %d, want 2", store.listed[0].Limit)
	}
}

// A failure to list is the sweep's own failure, not a per-run one: the loop
// restarts rather than silently sweeping nothing forever.
func TestRunRetentionReturnsListFailure(t *testing.T) {
	retention, store, _, _ := newRetentionForTest(t, time.Hour)
	store.listErr = errors.New("postgres unavailable")

	if _, err := retention.Sweep(t.Context()); err == nil {
		t.Fatal("Sweep returned no error when the list failed")
	}
}

// A refused run cannot always undo its own upload -- the process can die between
// the upload and the refusal -- so the sweep has to be able to find what a
// refusal left behind. Without this pass the object is unreachable from every
// other query in the system and stays in storage forever.
func TestRunRetentionSweepsOrphanedRunArtifacts(t *testing.T) {
	retention, store, objects, log := newRetentionForTest(t, 24*time.Hour)
	store.orphans = []OrphanedArtifactRecord{
		{ID: 7, StorageKey: "run/1/leaked"},
		{ID: 8, StorageKey: "run/1/alsolaked"},
	}

	result, err := retention.Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep returned error: %v", err)
	}
	if result.OrphanedArtifacts != 2 {
		t.Fatalf("result = %+v, want 2 orphaned artifacts removed", result)
	}
	if len(objects.deleted) != 2 {
		t.Fatalf("objects deleted = %v, want both orphaned objects", objects.deleted)
	}
	if len(store.deletedArts) != 2 {
		t.Fatalf("artifact rows deleted = %v, want both", store.deletedArts)
	}
	// The object has to go before the row, exactly as for an expired run.
	if log.events[0] != "delete_object" || log.events[1] != "delete_artifact" {
		t.Fatalf("events = %v, want the object removed before the artifact row", log.events)
	}
}

// An orphan whose object cannot be removed keeps its row, so the next sweep
// retries it rather than losing track of it.
func TestRunRetentionKeepsOrphanRowWhenObjectDeleteFails(t *testing.T) {
	retention, store, objects, _ := newRetentionForTest(t, 24*time.Hour)
	store.orphans = []OrphanedArtifactRecord{{ID: 7, StorageKey: "run/1/leaked"}}
	objects.deleteFn = func(string) error { return errors.New("storage unavailable") }

	result, err := retention.Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep returned error: %v", err)
	}
	if result.OrphanedArtifacts != 0 || len(store.deletedArts) != 0 {
		t.Fatalf("result = %+v deletedArts=%v, want the row kept for a retry", result, store.deletedArts)
	}
}

// An empty sweep must not be recorded. The shared counter floors a zero count at
// one, so recording it would read as "one run deleted" on a dashboard -- a number
// an operator has no reason to doubt.
func TestRunRetentionRecordsNothingWhenNothingExpired(t *testing.T) {
	retention, _, _, _ := newRetentionForTest(t, time.Hour)
	recorder := &retentionMetricsStub{}
	retention.metrics = recorder

	if _, err := retention.Sweep(t.Context()); err != nil {
		t.Fatalf("Sweep returned error: %v", err)
	}
	if len(recorder.actions) != 0 {
		t.Fatalf("recorded actions = %v, want none", recorder.actions)
	}
}

func TestRunRetentionRecordsWhatItRemoved(t *testing.T) {
	retention, store, _, _ := newRetentionForTest(t, time.Hour,
		ExpiredRunRecord{ID: 1, SourceArtifactID: int64Ptr(4), StorageKey: "run/1/abc"})
	store.orphans = []OrphanedArtifactRecord{{ID: 7, StorageKey: "run/1/leaked"}}
	recorder := &retentionMetricsStub{}
	retention.metrics = recorder

	if _, err := retention.Sweep(t.Context()); err != nil {
		t.Fatalf("Sweep returned error: %v", err)
	}
	want := []string{"run_retention/success/1", "run_retention_orphans/success/1"}
	if len(recorder.actions) != len(want) {
		t.Fatalf("recorded actions = %v, want %v", recorder.actions, want)
	}
	for i, expected := range want {
		if recorder.actions[i] != expected {
			t.Fatalf("recorded actions = %v, want %v", recorder.actions, want)
		}
	}
}

type retentionMetricsStub struct {
	actions []string
}

func (s *retentionMetricsStub) RecordReconcilerAction(action, result string, count int) {
	s.actions = append(s.actions, fmt.Sprintf("%s/%s/%d", action, result, count))
}
