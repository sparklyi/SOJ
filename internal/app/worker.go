package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"SOJ/internal/config"
	"SOJ/internal/contest"
	"SOJ/internal/httpapi"
	"SOJ/internal/observability"
	"SOJ/internal/postgres"
	"SOJ/internal/postgres/db"
	"SOJ/internal/problem"
	"SOJ/internal/queue"
	"SOJ/internal/storage"
	"SOJ/internal/submission"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func RunWorker(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "recover-dead-task" {
		return runWorkerRecoverDeadTask(ctx, args[1:], stdout)
	}

	fs := flag.NewFlagSet("soj-worker", flag.ContinueOnError)
	fs.SetOutput(stdout)
	healthAddr := fs.String("health-addr", "", "worker health HTTP listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *healthAddr != "" {
		cfg.Worker.HealthAddr = *healthAddr
	}

	logger := observability.NewLogger(cfg.Log.Level, stdout)
	tracing, err := setupProcessTracing(ctx, cfg, "soj-worker")
	if err != nil {
		return err
	}
	defer shutdownProcessTracing(ctx, cfg.Worker.ShutdownTimeout, logger, tracing)
	metrics := observability.NewMetrics("soj-worker")
	pool, err := postgres.OpenPool(ctx, postgres.PoolConfig{DSN: cfg.Database.DSN})
	if err != nil {
		return err
	}
	defer pool.Close()

	objectStore, err := newWorkerObjectStorage(cfg.Storage)
	if err != nil {
		return err
	}
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr})
	defer func() {
		_ = redisClient.Close()
	}()

	queries := db.New(pool)
	submissionRepo := submission.NewSQLRepositoryWithTxRunner(queries, pool)
	problemService := problem.NewService(problem.NewPostgresRepository(pool), objectStore)
	testcaseResolver := submission.NewTestcaseSnapshotResolver(queries, objectStore)
	taskQueue := queue.NewRedisStreamQueue(redisClient, queue.RedisStreamConfig{
		Stream:     cfg.Redis.Stream,
		Group:      cfg.Redis.Group,
		Consumer:   workerConsumerName(),
		MaxLen:     cfg.Redis.StreamMaxLen,
		DeadMaxLen: cfg.Redis.DeadStreamMaxLen,
	})
	if err := taskQueue.Ensure(ctx); err != nil {
		return err
	}
	resultQueue := queue.NewRedisStreamQueue(redisClient, queue.RedisStreamConfig{
		Stream:     envOr("SOJ_JUDGE_RESULT_STREAM", cfg.Redis.Stream+":results"),
		Group:      envOr("SOJ_JUDGE_RESULT_GROUP", "judge-result-consumers"),
		Consumer:   workerConsumerName(),
		StartID:    "0",
		MaxLen:     cfg.Redis.StreamMaxLen,
		DeadMaxLen: cfg.Redis.DeadStreamMaxLen,
	})
	if err := resultQueue.Ensure(ctx); err != nil {
		return err
	}
	judgeEngine := newJudgeEngine(cfg.Judge)
	sourceStore := submission.NewObjectSourceStore(objectStore)
	worker := submission.NewWorker(submission.WorkerOptions{
		Repository:       submissionRepo,
		Queue:            taskQueue,
		Judge:            judgeEngine,
		ProblemReader:    problemService,
		TestcaseResolver: testcaseResolver,
		SourceStore:      sourceStore,
		Metrics:          metrics,
	})
	resultConsumer := submission.NewResultConsumer(submission.ResultConsumerOptions{Repository: submissionRepo})
	reconciler := submission.NewReconciler(submissionRepo, worker, nil, metrics)
	contestService := contest.NewService(contest.NewPostgresRepository(pool))

	readiness := newWorkerReadiness(pool.Ping, taskQueue, resultQueue, objectStore, metrics)
	router := httpapi.NewRouter(httpapi.RouterOptions{
		Metrics:        metrics,
		ReadyCheck:     readiness.Check,
		TracingEnabled: tracing.Enabled(),
		TracingService: tracing.ServiceName(),
	})
	logger.InfoContext(ctx, "starting soj worker", "health_addr", cfg.Worker.HealthAddr, "request_stream", cfg.Redis.Stream, "request_group", cfg.Redis.Group, "result_stream", envOr("SOJ_JUDGE_RESULT_STREAM", cfg.Redis.Stream+":results"), "result_group", envOr("SOJ_JUDGE_RESULT_GROUP", "judge-result-consumers"))

	server := &http.Server{
		Addr:         cfg.Worker.HealthAddr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 2)
	go func() {
		errCh <- runHTTPServer(runCtx, server, cfg.Worker.ShutdownTimeout)
	}()
	go func() {
		errCh <- runWorkerLoops(runCtx, worker, resultConsumer, taskQueue, resultQueue, reconciler, contestService, metrics, cfg.Redis.BatchSize, cfg.Redis.Block)
	}()

	err = <-errCh
	cancel()
	if err == nil || err == context.Canceled || err == context.DeadlineExceeded {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	return err
}

type workerReconciler interface {
	ClaimStaleTasks(context.Context, time.Duration, int) (int, error)
	ResetStaleTasks(context.Context, time.Duration) (int, error)
	MarkStaleRuns(context.Context, time.Duration) (int, error)
}

type scoreSnapshotGenerator interface {
	GenerateDueScoreSnapshots(context.Context, int32) (contest.ScoreSnapshotGenerationResult, error)
}

func runWorkerLoops(ctx context.Context, worker *submission.Worker, resultConsumer *submission.ResultConsumer, requestQueue, resultQueue queue.TaskQueue, reconciler workerReconciler, snapshots scoreSnapshotGenerator, metrics workerLoopMetrics, batchSize int, block time.Duration) error {
	errCh := make(chan error, 3)
	go func() {
		errCh <- runDispatchLoop(ctx, worker, requestQueue, batchSize, dispatchInterval(block), metrics)
	}()
	go func() {
		errCh <- runResultConsumerLoop(ctx, resultConsumer, resultQueue, batchSize, block, metrics)
	}()
	go func() {
		errCh <- runReconcilerLoop(ctx, reconciler, snapshots)
	}()
	err := <-errCh
	if err == context.Canceled || err == context.DeadlineExceeded {
		return nil
	}
	return err
}

type workerLoopMetrics interface {
	ObserveQueueStats(queueName string, depth, pending int64, oldestPendingAge time.Duration)
	RecordResultConsumerProcess(queueName, result string, duration time.Duration)
}

func runDispatchLoop(ctx context.Context, worker *submission.Worker, requestQueue queue.TaskQueue, batchSize int, interval time.Duration, metrics workerLoopMetrics) error {
	if batchSize <= 0 {
		batchSize = 16
	}
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		recordQueueStats(ctx, metrics, "request", requestQueue)
		loopCtx, span := observability.Tracer("SOJ/internal/app").Start(ctx, "worker.dispatch", trace.WithAttributes(attribute.Int("soj.worker.batch_size", batchSize)))
		dispatched, err := worker.DispatchPending(loopCtx, int32(batchSize))
		span.SetAttributes(attribute.Int("soj.worker.dispatched", int(dispatched)))
		if err != nil {
			span.SetStatus(codes.Error, "dispatch_error")
			span.End()
			return err
		}
		span.End()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func runResultConsumerLoop(ctx context.Context, consumer *submission.ResultConsumer, resultQueue queue.TaskQueue, batchSize int, block time.Duration, metrics workerLoopMetrics) error {
	if batchSize <= 0 {
		batchSize = 16
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		recordQueueStats(ctx, metrics, "result", resultQueue)
		consumeCtx, consumeSpan := observability.Tracer("SOJ/internal/app").Start(ctx, "worker.result.consume", trace.WithAttributes(attribute.Int("soj.worker.batch_size", batchSize)))
		messages, err := resultQueue.Consume(consumeCtx, batchSize, block)
		consumeSpan.SetAttributes(attribute.Int("soj.worker.messages", len(messages)))
		if err != nil {
			consumeSpan.SetStatus(codes.Error, "consume_error")
			consumeSpan.End()
			return err
		}
		consumeSpan.End()
		for _, message := range messages {
			started := time.Now()
			messageCtx, span := observability.Tracer("SOJ/internal/app").Start(ctx, "worker.result.process")
			if err := consumer.ProcessResultMessage(messageCtx, message, resultQueue); err != nil {
				span.SetStatus(codes.Error, "process_error")
				span.End()
				recordResultConsumerProcess(metrics, "error", time.Since(started))
				return err
			}
			span.End()
			recordResultConsumerProcess(metrics, "success", time.Since(started))
		}
	}
}

func recordQueueStats(ctx context.Context, metrics workerLoopMetrics, queueName string, taskQueue queue.TaskQueue) {
	if metrics == nil {
		return
	}
	statsProvider, ok := taskQueue.(queue.QueueStatsProvider)
	if !ok {
		return
	}
	stats, err := statsProvider.Stats(ctx)
	if err != nil {
		return
	}
	metrics.ObserveQueueStats(queueName, stats.Depth, stats.Pending, stats.OldestPendingAge)
}

func recordResultConsumerProcess(metrics workerLoopMetrics, result string, duration time.Duration) {
	if metrics == nil {
		return
	}
	metrics.RecordResultConsumerProcess("result", result, duration)
}

func dispatchInterval(block time.Duration) time.Duration {
	if block > 0 && block < time.Second {
		return block
	}
	return time.Second
}

func runReconcilerLoop(ctx context.Context, reconciler workerReconciler, snapshots scoreSnapshotGenerator) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		loopCtx, span := observability.Tracer("SOJ/internal/app").Start(ctx, "worker.reconciler")
		if _, err := reconciler.ClaimStaleTasks(loopCtx, 5*time.Minute, 16); err != nil {
			span.SetStatus(codes.Error, "claim_stale_tasks_error")
			span.End()
			return err
		}
		if _, err := reconciler.ResetStaleTasks(loopCtx, 30*time.Minute); err != nil {
			span.SetStatus(codes.Error, "reset_stale_tasks_error")
			span.End()
			return err
		}
		if _, err := reconciler.MarkStaleRuns(loopCtx, 30*time.Minute); err != nil {
			span.SetStatus(codes.Error, "mark_stale_runs_error")
			span.End()
			return err
		}
		if snapshots != nil {
			if _, err := snapshots.GenerateDueScoreSnapshots(loopCtx, 16); err != nil {
				span.SetStatus(codes.Error, "score_snapshot_error")
				span.End()
				return err
			}
		}
		span.End()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func newWorkerObjectStorage(cfg config.StorageConfig) (storage.ObjectStorage, error) {
	return storage.NewS3Storage(storage.S3Options{
		Endpoint:        cfg.Endpoint,
		AccessKeyID:     cfg.AccessKey,
		SecretAccessKey: cfg.SecretKey,
		Bucket:          cfg.Bucket,
		Region:          cfg.Region,
		PathStyle:       cfg.UsePathStyle,
	})
}

func workerConsumerName() string {
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return hostname
	}
	return "worker"
}

func runWorkerRecoverDeadTask(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("soj-worker recover-dead-task", flag.ContinueOnError)
	fs.SetOutput(stdout)
	taskID := fs.Int64("task-id", 0, "dead judge task id to recover")
	reason := fs.String("reason", "manual dead task recovery", "operator-visible recovery reason")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *taskID <= 0 {
		return errors.New("task-id is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := postgres.OpenPool(ctx, postgres.PoolConfig{DSN: cfg.Database.DSN})
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := submission.NewSQLRepository(db.New(pool))
	task, err := repo.RecoverDeadJudgeTask(ctx, *taskID, time.Now().UTC(), *reason)
	if err != nil {
		return err
	}
	if stdout != nil {
		_, _ = fmt.Fprintf(stdout, "recovered judge task %d for submission %d as %s\n", task.ID, task.SubmissionID, task.Status)
	}
	return nil
}
