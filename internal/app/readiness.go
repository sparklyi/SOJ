package app

import (
	"context"

	"SOJ/internal/observability"
	"SOJ/internal/queue"
	"SOJ/internal/storage"
)

func newWorkerReadiness(dbCheck observability.CheckFunc, requestQueue, runQueue, resultQueue queue.TaskQueue, objectStore storage.ObjectStorage, metrics observability.ReadinessMetrics) observability.Readiness {
	return observability.NewReadinessWithMetrics(map[string]observability.CheckFunc{
		"postgres":             dbCheck,
		"redis.request_stream": func(ctx context.Context) error { return queue.CheckReady(ctx, requestQueue) },
		"redis.run_stream":     func(ctx context.Context) error { return queue.CheckReady(ctx, runQueue) },
		"redis.result_stream":  func(ctx context.Context) error { return queue.CheckReady(ctx, resultQueue) },
		"object_storage":       func(ctx context.Context) error { return storage.CheckReady(ctx, objectStore) },
	}, metrics)
}

// judgeRequestStreamNames lists the consumed stream names for the startup log.
func judgeRequestStreamNames(streams []judgeRequestStream) []string {
	names := make([]string, 0, len(streams))
	for _, stream := range streams {
		names = append(names, stream.Name)
	}
	return names
}

func newJudgeAgentReadiness(requestStreams []judgeRequestStream, resultQueue queue.TaskQueue, objectStore storage.ObjectStorage, sandboxCheck observability.CheckFunc, metrics observability.ReadinessMetrics) observability.Readiness {
	if sandboxCheck == nil {
		sandboxCheck = func(context.Context) error { return nil }
	}
	checks := map[string]observability.CheckFunc{
		"redis.result_stream": func(ctx context.Context) error { return queue.CheckReady(ctx, resultQueue) },
		"object_storage":      func(ctx context.Context) error { return storage.CheckReady(ctx, objectStore) },
		"sandbox":             sandboxCheck,
	}
	// Every consumed stream is a readiness dependency: an agent that cannot reach
	// the runs stream must not report itself ready to take work.
	for _, stream := range requestStreams {
		checks["redis.request_stream."+stream.Name] = func(ctx context.Context) error {
			return queue.CheckReady(ctx, stream.Queue)
		}
	}
	return observability.NewReadinessWithMetrics(checks, metrics)
}
