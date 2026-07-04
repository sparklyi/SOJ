package submission

import (
	"context"
	"time"
)

type Reconciler struct {
	repo   Repository
	worker *Worker
	now    func() time.Time
}

func NewReconciler(repo Repository, worker *Worker, now func() time.Time) *Reconciler {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Reconciler{repo: repo, worker: worker, now: now}
}

func (r *Reconciler) ClaimStaleTasks(ctx context.Context, minIdle time.Duration, limit int) (int, error) {
	messages, err := r.worker.queue.ClaimStale(ctx, minIdle, limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, message := range messages {
		if err := r.worker.ProcessMessage(ctx, message); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (r *Reconciler) MarkStaleRuns(ctx context.Context, maxAge time.Duration) (int, error) {
	runs, err := r.repo.MarkStaleRunsSystemError(ctx, r.now().Add(-maxAge), "run reconciliation marked stale run as system_error")
	if err != nil {
		return 0, err
	}
	return len(runs), nil
}

func (r *Reconciler) ResetStaleTasks(ctx context.Context, maxAge time.Duration) (int, error) {
	tasks, err := r.repo.ResetStaleJudgeTasks(ctx, r.now().Add(-maxAge), "judge task reconciliation reset stale task to pending")
	if err != nil {
		return 0, err
	}
	return len(tasks), nil
}
