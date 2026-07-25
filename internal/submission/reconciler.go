package submission

import (
	"context"
	"time"

	"SOJ/internal/queue"
)

type reconciliationStore interface {
	MarkStaleRunsSystemError(context.Context, time.Time, string) ([]RunRecord, error)
	ResetStaleJudgeTasks(context.Context, time.Time, string) ([]JudgeTaskRecord, error)
}

type taskMessageProcessor interface {
	ProcessMessage(context.Context, queue.Message) error
}

type Reconciler struct {
	store   reconciliationStore
	queue   staleTaskClaimer
	process taskMessageProcessor
	now     func() time.Time
	metrics ReconcilerMetrics
}

type ReconcilerMetrics interface {
	RecordReconcilerAction(action, result string, count int)
}

func NewReconciler(store reconciliationStore, taskQueue staleTaskClaimer, processor taskMessageProcessor, now func() time.Time, metrics ...ReconcilerMetrics) *Reconciler {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	var recorder ReconcilerMetrics
	if len(metrics) > 0 {
		recorder = metrics[0]
	}
	return &Reconciler{store: store, queue: taskQueue, process: processor, now: now, metrics: recorder}
}

func (r *Reconciler) ClaimStaleTasks(ctx context.Context, minIdle time.Duration, limit int) (int, error) {
	messages, err := r.queue.ClaimStale(ctx, minIdle, limit)
	if err != nil {
		r.record("claim_stale_tasks", "error", 1)
		return 0, err
	}
	processed := 0
	for _, message := range messages {
		if err := r.process.ProcessMessage(ctx, message); err != nil {
			r.record("claim_stale_tasks", "error", 1)
			return processed, err
		}
		processed++
	}
	r.record("claim_stale_tasks", "success", processed)
	return processed, nil
}

func (r *Reconciler) MarkStaleRuns(ctx context.Context, maxAge time.Duration) (int, error) {
	runs, err := r.store.MarkStaleRunsSystemError(ctx, r.now().Add(-maxAge), "run reconciliation marked stale run as system_error")
	if err != nil {
		r.record("mark_stale_runs", "error", 1)
		return 0, err
	}
	r.record("mark_stale_runs", "success", len(runs))
	return len(runs), nil
}

func (r *Reconciler) ResetStaleTasks(ctx context.Context, maxAge time.Duration) (int, error) {
	tasks, err := r.store.ResetStaleJudgeTasks(ctx, r.now().Add(-maxAge), "judge task reconciliation reset stale task to pending")
	if err != nil {
		r.record("reset_stale_tasks", "error", 1)
		return 0, err
	}
	r.record("reset_stale_tasks", "success", len(tasks))
	return len(tasks), nil
}

func (r *Reconciler) record(action, result string, count int) {
	if r.metrics != nil {
		r.metrics.RecordReconcilerAction(action, result, count)
	}
}
