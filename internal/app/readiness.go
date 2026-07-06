package app

import (
	"context"

	"SOJ/internal/observability"
	"SOJ/internal/queue"
	"SOJ/internal/storage"
)

func newWorkerReadiness(dbCheck observability.CheckFunc, requestQueue, resultQueue queue.TaskQueue, objectStore storage.ObjectStorage, metrics observability.ReadinessMetrics) observability.Readiness {
	return observability.NewReadinessWithMetrics(map[string]observability.CheckFunc{
		"postgres":             dbCheck,
		"redis.request_stream": func(ctx context.Context) error { return queue.CheckReady(ctx, requestQueue) },
		"redis.result_stream":  func(ctx context.Context) error { return queue.CheckReady(ctx, resultQueue) },
		"object_storage":       func(ctx context.Context) error { return storage.CheckReady(ctx, objectStore) },
	}, metrics)
}

func newJudgeAgentReadiness(requestQueue, resultQueue queue.TaskQueue, objectStore storage.ObjectStorage, sandboxCheck observability.CheckFunc, metrics observability.ReadinessMetrics) observability.Readiness {
	if sandboxCheck == nil {
		sandboxCheck = func(context.Context) error { return nil }
	}
	return observability.NewReadinessWithMetrics(map[string]observability.CheckFunc{
		"redis.request_stream": func(ctx context.Context) error { return queue.CheckReady(ctx, requestQueue) },
		"redis.result_stream":  func(ctx context.Context) error { return queue.CheckReady(ctx, resultQueue) },
		"object_storage":       func(ctx context.Context) error { return storage.CheckReady(ctx, objectStore) },
		"sandbox":              sandboxCheck,
	}, metrics)
}
