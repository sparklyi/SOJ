package submission

import (
	"context"
	"testing"
	"time"
)

type reconciliationStoreStub struct{}

func (reconciliationStoreStub) MarkStaleRunsSystemError(context.Context, time.Time, string) ([]RunRecord, error) {
	return []RunRecord{{ID: 1}}, nil
}

func (reconciliationStoreStub) ResetStaleJudgeTasks(context.Context, time.Time, string) ([]JudgeTaskRecord, error) {
	return []JudgeTaskRecord{{ID: 1}}, nil
}

func TestReconcilerDependsOnlyOnReconciliationStore(t *testing.T) {
	now := time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC)
	reconciler := NewReconciler(reconciliationStoreStub{}, nil, func() time.Time { return now })

	runs, err := reconciler.MarkStaleRuns(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("MarkStaleRuns() error = %v", err)
	}
	if runs != 1 {
		t.Fatalf("MarkStaleRuns() count = %d, want 1", runs)
	}

	tasks, err := reconciler.ResetStaleTasks(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("ResetStaleTasks() error = %v", err)
	}
	if tasks != 1 {
		t.Fatalf("ResetStaleTasks() count = %d, want 1", tasks)
	}
}
