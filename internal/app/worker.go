package app

import (
	"context"
	"flag"
	"io"
	"net/http"
	"os"
	"time"

	"SOJ/internal/config"
	"SOJ/internal/httpapi"
	"SOJ/internal/observability"
	"SOJ/internal/postgres"
	"SOJ/internal/postgres/db"
	"SOJ/internal/problem"
	"SOJ/internal/queue"
	"SOJ/internal/storage"
	"SOJ/internal/submission"

	"github.com/redis/go-redis/v9"
)

func RunWorker(ctx context.Context, args []string, stdout, stderr io.Writer) error {
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
		Stream:   cfg.Redis.Stream,
		Group:    cfg.Redis.Group,
		Consumer: workerConsumerName(),
	})
	if err := taskQueue.Ensure(ctx); err != nil {
		return err
	}
	resultQueue := queue.NewRedisStreamQueue(redisClient, queue.RedisStreamConfig{
		Stream:   envOr("SOJ_JUDGE_RESULT_STREAM", cfg.Redis.Stream+":results"),
		Group:    envOr("SOJ_JUDGE_RESULT_GROUP", "judge-result-consumers"),
		Consumer: workerConsumerName(),
		StartID:  "0",
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
	reconciler := submission.NewReconciler(submissionRepo, worker, nil)

	router := httpapi.NewRouter(httpapi.RouterOptions{Metrics: metrics})
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
		errCh <- runWorkerLoops(runCtx, worker, resultConsumer, resultQueue, reconciler, cfg.Redis.BatchSize, cfg.Redis.Block)
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

func runWorkerLoops(ctx context.Context, worker *submission.Worker, resultConsumer *submission.ResultConsumer, resultQueue queue.TaskQueue, reconciler *submission.Reconciler, batchSize int, block time.Duration) error {
	errCh := make(chan error, 3)
	go func() {
		errCh <- runDispatchLoop(ctx, worker, batchSize, dispatchInterval(block))
	}()
	go func() {
		errCh <- runResultConsumerLoop(ctx, resultConsumer, resultQueue, batchSize, block)
	}()
	go func() {
		errCh <- runReconcilerLoop(ctx, reconciler)
	}()
	err := <-errCh
	if err == context.Canceled || err == context.DeadlineExceeded {
		return nil
	}
	return err
}

func runDispatchLoop(ctx context.Context, worker *submission.Worker, batchSize int, interval time.Duration) error {
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
		if _, err := worker.DispatchPending(ctx, int32(batchSize)); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func runResultConsumerLoop(ctx context.Context, consumer *submission.ResultConsumer, resultQueue queue.TaskQueue, batchSize int, block time.Duration) error {
	if batchSize <= 0 {
		batchSize = 16
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		messages, err := resultQueue.Consume(ctx, batchSize, block)
		if err != nil {
			return err
		}
		for _, message := range messages {
			if err := consumer.ProcessResultMessage(ctx, message, resultQueue); err != nil {
				return err
			}
		}
	}
}

func dispatchInterval(block time.Duration) time.Duration {
	if block > 0 && block < time.Second {
		return block
	}
	return time.Second
}

func runReconcilerLoop(ctx context.Context, reconciler *submission.Reconciler) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := reconciler.ClaimStaleTasks(ctx, 5*time.Minute, 16); err != nil {
			return err
		}
		if _, err := reconciler.ResetStaleTasks(ctx, 30*time.Minute); err != nil {
			return err
		}
		if _, err := reconciler.MarkStaleRuns(ctx, 30*time.Minute); err != nil {
			return err
		}
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
