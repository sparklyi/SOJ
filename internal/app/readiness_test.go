package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"SOJ/internal/contest"
	"SOJ/internal/queue"
	"SOJ/internal/storage"
)

func TestWorkerReadinessChecksRuntimeDependencies(t *testing.T) {
	requestQueue := &readyQueue{}
	resultQueue := &readyQueue{}
	store := &readyObjectStore{}
	readiness := newWorkerReadiness(
		func(context.Context) error { return nil },
		requestQueue,
		resultQueue,
		store,
		nil,
	)

	if err := readiness.Check(context.Background()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !requestQueue.called || !resultQueue.called || !store.called {
		t.Fatalf("readiness did not check all dependencies: request=%v result=%v storage=%v", requestQueue.called, resultQueue.called, store.called)
	}
}

func TestJudgeAgentReadinessReportsSandboxProbeFailure(t *testing.T) {
	wantErr := errors.New("runsc unavailable")
	readiness := newJudgeAgentReadiness(
		&readyQueue{},
		&readyQueue{},
		&readyObjectStore{},
		func(context.Context) error { return wantErr },
		nil,
	)

	err := readiness.Check(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Check() error = %v, want %v", err, wantErr)
	}
}

func TestRunWorkerRecoverDeadTaskRequiresTaskID(t *testing.T) {
	err := RunWorker(context.Background(), []string{"recover-dead-task"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "task-id is required") {
		t.Fatalf("RunWorker error = %v, want task-id requirement", err)
	}
}

func TestRunReconcilerLoopGeneratesDueScoreSnapshots(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reconciler := &recordingWorkerReconciler{}
	snapshots := &recordingScoreSnapshotGenerator{afterCall: cancel}

	err := runReconcilerLoop(ctx, reconciler, snapshots)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runReconcilerLoop error = %v, want context canceled", err)
	}
	if !reconciler.claimed || !reconciler.reset || !reconciler.markedRuns {
		t.Fatalf("reconciler calls = %+v, want all reconciliation actions", reconciler)
	}
	if snapshots.calls != 1 || snapshots.limit != 16 {
		t.Fatalf("snapshot generator calls=%d limit=%d, want one call with limit 16", snapshots.calls, snapshots.limit)
	}
}

type recordingWorkerReconciler struct {
	claimed    bool
	reset      bool
	markedRuns bool
}

func (r *recordingWorkerReconciler) ClaimStaleTasks(context.Context, time.Duration, int) (int, error) {
	r.claimed = true
	return 0, nil
}

func (r *recordingWorkerReconciler) ResetStaleTasks(context.Context, time.Duration) (int, error) {
	r.reset = true
	return 0, nil
}

func (r *recordingWorkerReconciler) MarkStaleRuns(context.Context, time.Duration) (int, error) {
	r.markedRuns = true
	return 0, nil
}

type recordingScoreSnapshotGenerator struct {
	calls     int
	limit     int32
	afterCall func()
}

func (g *recordingScoreSnapshotGenerator) GenerateDueScoreSnapshots(ctx context.Context, limit int32) (contest.ScoreSnapshotGenerationResult, error) {
	g.calls++
	g.limit = limit
	if g.afterCall != nil {
		g.afterCall()
	}
	return contest.ScoreSnapshotGenerationResult{}, nil
}

type readyQueue struct {
	called bool
	err    error
}

func (q *readyQueue) Ensure(context.Context) error { return nil }

func (q *readyQueue) Publish(context.Context, int64, []byte) (string, error) {
	return "", nil
}

func (q *readyQueue) Consume(context.Context, int, time.Duration) ([]queue.Message, error) {
	return nil, nil
}

func (q *readyQueue) ClaimStale(context.Context, time.Duration, int) ([]queue.Message, error) {
	return nil, nil
}

func (q *readyQueue) Ack(context.Context, string) error { return nil }

func (q *readyQueue) DeadLetter(context.Context, queue.Message, string) error {
	return nil
}

func (q *readyQueue) Close() error { return nil }

func (q *readyQueue) Ready(context.Context) error {
	q.called = true
	return q.err
}

type readyObjectStore struct {
	called bool
	err    error
}

func (s *readyObjectStore) Put(context.Context, storage.Object) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, nil
}

func (s *readyObjectStore) Get(context.Context, string) (io.ReadCloser, storage.ObjectInfo, error) {
	return io.NopCloser(strings.NewReader("")), storage.ObjectInfo{}, nil
}

func (s *readyObjectStore) Delete(context.Context, string) error { return nil }

func (s *readyObjectStore) Stat(context.Context, string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, nil
}

func (s *readyObjectStore) Ready(context.Context) error {
	s.called = true
	return s.err
}
