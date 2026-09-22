package submission

import (
	"context"
	"time"
)

// ExpiredRunRecord is a run that retention may remove, with the object that has
// to go before it.
type ExpiredRunRecord struct {
	ID               int64
	SourceArtifactID *int64
	// StorageKey is empty when the run's artifact is already gone. The row still
	// has to be removed; there is just nothing left to delete from storage.
	StorageKey string
}

type ListExpiredRunsInput struct {
	CreatedBefore time.Time
	Limit         int32
}

type DeleteExpiredRunInput struct {
	RunID      int64
	ArtifactID *int64
}

// OrphanedArtifactRecord is a run source object that no run points at.
type OrphanedArtifactRecord struct {
	ID         int64
	StorageKey string
}

// runRetentionStore is what a sweep needs: what has expired, and the ability to
// remove it.
type runRetentionStore interface {
	ListExpiredRuns(context.Context, ListExpiredRunsInput) ([]ExpiredRunRecord, error)
	DeleteExpiredRun(context.Context, DeleteExpiredRunInput) error
	ListOrphanedRunArtifacts(context.Context, ListOrphanedArtifactsInput) ([]OrphanedArtifactRecord, error)
	DeleteArtifact(context.Context, int64) error
}

type ListOrphanedArtifactsInput struct {
	CreatedBefore time.Time
	Limit         int32
}

// objectDeleter is the one thing retention asks of storage.
type objectDeleter interface {
	Delete(ctx context.Context, key string) error
}

type RunRetentionMetrics interface {
	RecordReconcilerAction(action, result string, count int)
}

// RunRetention deletes self-runs and their source objects once they are past the
// retention window.
//
// Self-runs are scratch work: unlike a submission, nothing about them has to
// survive, and each one writes a source object to storage. Without this the
// playground grows object storage without bound, which is a worse leak than the
// rows themselves because nothing else will ever notice it.
type RunRetention struct {
	store    runRetentionStore
	objects  objectDeleter
	now      func() time.Time
	age      time.Duration
	interval time.Duration
	batch    int
	metrics  RunRetentionMetrics
}

type RunRetentionOptions struct {
	Store   runRetentionStore
	Objects objectDeleter
	Now     func() time.Time
	// Age is how long a finished run is kept. Zero or less disables the sweep.
	Age time.Duration
	// Interval is how often the sweep should run. It lives here rather than at
	// the call site because the cadence and the cost of a sweep are the same
	// decision.
	Interval time.Duration
	Batch    int
	Metrics  RunRetentionMetrics
}

type RunRetentionResult struct {
	Scanned int
	Deleted int
	Failed  int
	// OrphanedArtifacts counts source objects removed that no run pointed at.
	OrphanedArtifacts int
}

func NewRunRetention(options RunRetentionOptions) *RunRetention {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	batch := options.Batch
	if batch <= 0 {
		batch = defaultRunRetentionBatch
	}
	interval := options.Interval
	if interval <= 0 {
		interval = defaultRunRetentionInterval
	}
	return &RunRetention{
		store:    options.Store,
		objects:  options.Objects,
		now:      now,
		age:      options.Age,
		interval: interval,
		batch:    batch,
		metrics:  options.Metrics,
	}
}

// Enabled reports whether the sweep has a window to work with.
func (r *RunRetention) Enabled() bool {
	return r.age > 0
}

// Interval is how often the sweep should run.
func (r *RunRetention) Interval() time.Duration {
	return r.interval
}

// Sweep removes one batch of expired runs.
//
// A failure on a single run is counted and skipped rather than aborting the
// batch: one unreadable object should not stop the other hundred from being
// cleaned, and the next sweep retries it.
func (r *RunRetention) Sweep(ctx context.Context) (RunRetentionResult, error) {
	if !r.Enabled() {
		return RunRetentionResult{}, nil
	}
	expired, err := r.store.ListExpiredRuns(ctx, ListExpiredRunsInput{
		CreatedBefore: r.now().Add(-r.age),
		Limit:         int32(r.batch),
	})
	if err != nil {
		r.record("run_retention", "error", 1)
		return RunRetentionResult{}, err
	}
	result := RunRetentionResult{Scanned: len(expired)}
	for _, run := range expired {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := r.deleteRun(ctx, run); err != nil {
			result.Failed++
			continue
		}
		result.Deleted++
	}
	// Only record when something happened. RecordReconcilerAction floors a zero
	// count at one, so recording an empty sweep would read as "one run deleted"
	// -- which is exactly the kind of number an operator would trust.
	if result.Failed > 0 {
		r.record("run_retention", "error", result.Failed)
	}
	if result.Deleted > 0 {
		r.record("run_retention", "success", result.Deleted)
	}

	orphans, err := r.sweepOrphanedArtifacts(ctx)
	if err != nil {
		return result, err
	}
	result.OrphanedArtifacts = orphans
	return result, nil
}

// sweepOrphanedArtifacts removes run source objects that nothing points at.
//
// Refusing a run cannot always undo its own upload -- the process can die between
// the two -- so the sweep has to be able to find what a refusal left behind.
// Without this pass such an object is unreachable from every other query and
// stays in storage forever.
func (r *RunRetention) sweepOrphanedArtifacts(ctx context.Context) (int, error) {
	orphans, err := r.store.ListOrphanedRunArtifacts(ctx, ListOrphanedArtifactsInput{
		CreatedBefore: r.now().Add(-r.age),
		Limit:         int32(r.batch),
	})
	if err != nil {
		r.record("run_retention_orphans", "error", 1)
		return 0, err
	}
	removed := 0
	failed := 0
	for _, orphan := range orphans {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		// Same order as deleteRun, and for the same reason: the object goes
		// first, so a failure leaves the row that points at it for next time.
		if orphan.StorageKey != "" {
			if err := r.objects.Delete(ctx, orphan.StorageKey); err != nil {
				failed++
				continue
			}
		}
		if err := r.store.DeleteArtifact(ctx, orphan.ID); err != nil {
			failed++
			continue
		}
		removed++
	}
	if failed > 0 {
		r.record("run_retention_orphans", "error", failed)
	}
	if removed > 0 {
		r.record("run_retention_orphans", "success", removed)
	}
	return removed, nil
}

// deleteRun removes the source object before the row.
//
// This order is the whole point. The reverse would drop the row first and leave
// the object behind with nothing left pointing at it, which is an unfixable
// leak. As written, a failure leaves the row in place for the next sweep -- and
// deleting an object that is already gone is a no-op, so retrying is safe.
func (r *RunRetention) deleteRun(ctx context.Context, run ExpiredRunRecord) error {
	if run.StorageKey != "" {
		if err := r.objects.Delete(ctx, run.StorageKey); err != nil {
			return err
		}
	}
	return r.store.DeleteExpiredRun(ctx, DeleteExpiredRunInput{RunID: run.ID, ArtifactID: run.SourceArtifactID})
}

func (r *RunRetention) record(action, result string, count int) {
	if r.metrics != nil {
		r.metrics.RecordReconcilerAction(action, result, count)
	}
}
