package submission

import (
	"context"
	"time"
)

type Reconciler struct {
	repo    Repository
	worker  *Worker
	now     func() time.Time
	metrics ReconcilerMetrics
}

type ReconcilerMetrics interface {
	RecordReconcilerAction(action, result string, count int)
}

func NewReconciler(repo Repository, worker *Worker, now func() time.Time, metrics ...ReconcilerMetrics) *Reconciler {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	var recorder ReconcilerMetrics
	if len(metrics) > 0 {
		recorder = metrics[0]
	}
	return &Reconciler{repo: repo, worker: worker, now: now, metrics: recorder}
}

func (r *Reconciler) ClaimStaleTasks(ctx context.Context, minIdle time.Duration, limit int) (int, error) {
	messages, err := r.worker.queue.ClaimStale(ctx, minIdle, limit)
	if err != nil {
		r.record("claim_stale_tasks", "error", 1)
		return 0, err
	}
	processed := 0
	for _, message := range messages {
		if err := r.worker.ProcessMessage(ctx, message); err != nil {
			r.record("claim_stale_tasks", "error", 1)
			return processed, err
		}
		processed++
	}
	r.record("claim_stale_tasks", "success", processed)
	return processed, nil
}

func (r *Reconciler) MarkStaleRuns(ctx context.Context, maxAge time.Duration) (int, error) {
	runs, err := r.repo.MarkStaleRunsSystemError(ctx, r.now().Add(-maxAge), "run reconciliation marked stale run as system_error")
	if err != nil {
		r.record("mark_stale_runs", "error", 1)
		return 0, err
	}
	r.record("mark_stale_runs", "success", len(runs))
	return len(runs), nil
}

func (r *Reconciler) ResetStaleTasks(ctx context.Context, maxAge time.Duration) (int, error) {
	tasks, err := r.repo.ResetStaleJudgeTasks(ctx, r.now().Add(-maxAge), "judge task reconciliation reset stale task to pending")
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
