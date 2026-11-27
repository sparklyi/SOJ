package app

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"SOJ/internal/observability"
	"SOJ/internal/queue"
	"SOJ/internal/submission"
)

// errDispatchSentinel stops runDispatchLoop on its first dispatch so the test
// needs no timer, Redis, or database.
var errDispatchSentinel = errors.New("dispatch sentinel")

// statsQueue is a queue whose only behaviour is reporting fixed stats. Calling
// any other TaskQueue method panics through the nil embedded interface.
type statsQueue struct {
	queue.TaskQueue
	stats queue.QueueStats
}

func (q *statsQueue) Stats(context.Context) (queue.QueueStats, error) {
	return q.stats, nil
}

// failingDispatchStore fails the first claim of submission tasks, which makes
// DispatchPending return errDispatchSentinel before any publish happens.
type failingDispatchStore struct{}

func (failingDispatchStore) ClaimPendingJudgeTasks(context.Context, int32) ([]submission.JudgeTaskRecord, error) {
	return nil, errDispatchSentinel
}
func (failingDispatchStore) ClaimPendingRunTasks(context.Context, int32) ([]submission.JudgeTaskRecord, error) {
	return nil, errDispatchSentinel
}
func (failingDispatchStore) RetryJudgeTask(context.Context, int64, time.Time, string) (submission.JudgeTaskRecord, error) {
	return submission.JudgeTaskRecord{}, errDispatchSentinel
}
func (failingDispatchStore) MarkJudgeTaskDispatched(context.Context, int64, string) (submission.JudgeTaskRecord, error) {
	return submission.JudgeTaskRecord{}, errDispatchSentinel
}
func (failingDispatchStore) GetSubmission(context.Context, int64) (submission.SubmissionRecord, error) {
	return submission.SubmissionRecord{}, errDispatchSentinel
}
func (failingDispatchStore) GetRun(context.Context, int64) (submission.RunRecord, error) {
	return submission.RunRecord{}, errDispatchSentinel
}
func (failingDispatchStore) GetArtifact(context.Context, int64) (submission.ArtifactRecord, error) {
	return submission.ArtifactRecord{}, errDispatchSentinel
}
func (failingDispatchStore) GetLanguage(context.Context, int64) (submission.LanguageRecord, error) {
	return submission.LanguageRecord{}, errDispatchSentinel
}
func (failingDispatchStore) MarkSubmissionRunning(context.Context, int64) (submission.SubmissionRecord, error) {
	return submission.SubmissionRecord{}, errDispatchSentinel
}
func (failingDispatchStore) MarkRunRunning(context.Context, int64) (submission.RunRecord, error) {
	return submission.RunRecord{}, errDispatchSentinel
}
func (failingDispatchStore) EnsureJudgeAttempt(context.Context, submission.EnsureJudgeAttemptInput) (submission.JudgeAttemptRecord, error) {
	return submission.JudgeAttemptRecord{}, errDispatchSentinel
}

func TestDispatchLoopSamplesRequestAndRunQueuesSeparately(t *testing.T) {
	requestQueue := &statsQueue{stats: queue.QueueStats{Depth: 7, Pending: 3, OldestPendingAge: 45 * time.Second}}
	runQueue := &statsQueue{stats: queue.QueueStats{Depth: 11, Pending: 5, OldestPendingAge: 90 * time.Second}}
	worker := submission.NewWorker(
		submission.NewTaskDispatcher(submission.TaskDispatcherOptions{Store: failingDispatchStore{}}),
		nil,
		nil,
	)
	metrics := observability.NewMetrics("test")

	err := runDispatchLoop(context.Background(), worker, requestQueue, runQueue, 16, time.Hour, metrics)
	if !errors.Is(err, errDispatchSentinel) {
		t.Fatalf("runDispatchLoop() error = %v, want %v", err, errDispatchSentinel)
	}

	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()

	for _, want := range []string{
		`soj_queue_depth{queue="request",service="test"} 7`,
		`soj_queue_pending_messages{queue="request",service="test"} 3`,
		`soj_queue_oldest_pending_age_seconds{queue="request",service="test"} 45`,
		`soj_queue_depth{queue="run_request",service="test"} 11`,
		`soj_queue_pending_messages{queue="run_request",service="test"} 5`,
		`soj_queue_oldest_pending_age_seconds{queue="run_request",service="test"} 90`,
	} {
		if !hasMetricLine(body, want) {
			t.Fatalf("metrics body missing %s:\n%s", want, body)
		}
	}
}

// hasMetricLine reports whether body contains want as a complete line, so a
// sample value of 7 does not also match 70.
func hasMetricLine(body, want string) bool {
	for _, line := range strings.Split(body, "\n") {
		if line == want {
			return true
		}
	}
	return false
}
