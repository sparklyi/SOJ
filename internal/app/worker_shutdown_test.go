package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"SOJ/internal/queue"
	"SOJ/internal/submission"
)

func TestWorkerLoopShutdown(t *testing.T) {
	consumeErr := errors.New("result consumer failed")
	tests := []struct {
		name         string
		cancelParent bool
		wantErr      error
	}{
		{name: "loop error", wantErr: consumeErr},
		{name: "parent cancellation", cancelParent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				started := make(chan struct{})
				canceled := make(chan struct{})
				release := make(chan struct{})
				releaseReconciler := sync.OnceFunc(func() { close(release) })
				defer releaseReconciler()

				reconciler := &shutdownTestReconciler{started: started, canceled: canceled, release: release}
				resultQueue := shutdownTestQueue{consume: func(ctx context.Context) error {
					<-started
					if tt.cancelParent {
						<-ctx.Done()
						return ctx.Err()
					}
					return consumeErr
				}}
				dispatcher := submission.NewTaskDispatcher(submission.TaskDispatcherOptions{Store: shutdownTestDispatchStore{}})
				worker := submission.NewWorker(dispatcher, nil, nil)
				done := make(chan error, 1)
				go func() {
					done <- runWorkerLoops(ctx, worker, nil, nil, resultQueue, reconciler, nil, nil, nil, 1, time.Second, time.Second)
				}()
				<-started
				if tt.cancelParent {
					cancel()
				}
				synctest.Wait()
				select {
				case <-canceled:
				default:
					t.Error("reconciler did not receive cancellation")
				}
				select {
				case err := <-done:
					t.Fatalf("worker supervisor returned before the reconciler finished: %v", err)
				default:
				}

				releaseReconciler()
				if err := <-done; !errors.Is(err, tt.wantErr) {
					t.Fatalf("worker supervisor returned %v, want %v", err, tt.wantErr)
				}
			})
		})
	}
}

type shutdownTestDispatchStore struct{ *submission.SQLRepository }

func (shutdownTestDispatchStore) ClaimPendingJudgeTasks(context.Context, int32) ([]submission.JudgeTaskRecord, error) {
	return nil, nil
}

type shutdownTestQueue struct {
	queue.TaskQueue
	consume func(context.Context) error
}

func (q shutdownTestQueue) Consume(ctx context.Context, _ int, _ time.Duration) ([]queue.Message, error) {
	return nil, q.consume(ctx)
}

type shutdownTestReconciler struct {
	recordingWorkerReconciler
	started, canceled chan<- struct{}
	release           <-chan struct{}
}

func (r *shutdownTestReconciler) ClaimStaleTasks(ctx context.Context, _ time.Duration, _ int) (int, error) {
	close(r.started)
	<-ctx.Done()
	close(r.canceled)
	<-r.release
	return 0, ctx.Err()
}
