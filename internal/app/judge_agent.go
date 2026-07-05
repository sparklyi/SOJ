package app

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"os"
	"time"

	"SOJ/internal/config"
	"SOJ/internal/httpapi"
	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/observability"
	"SOJ/internal/queue"
	"SOJ/internal/storage"
	"SOJ/internal/submission"

	"github.com/redis/go-redis/v9"
)

func RunJudgeAgent(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("soj-judge-agent", flag.ContinueOnError)
	fs.SetOutput(stdout)
	healthAddr := fs.String("health-addr", envOr("SOJ_JUDGE_AGENT_HEALTH_ADDR", ":8082"), "judge agent health HTTP listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := observability.NewLogger(cfg.Log.Level, stdout)
	metrics := observability.NewMetrics("soj-judge-agent")

	objectStore, err := newJudgeAgentObjectStorage(cfg.Storage)
	if err != nil {
		return err
	}
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr})
	defer func() {
		_ = redisClient.Close()
	}()

	requestQueue := queue.NewRedisStreamQueue(redisClient, queue.RedisStreamConfig{
		Stream:   envOr("SOJ_JUDGE_REQUEST_STREAM", cfg.Redis.Stream),
		Group:    envOr("SOJ_JUDGE_AGENT_GROUP", "judge-agents"),
		Consumer: judgeAgentConsumerName(),
	})
	resultQueue := queue.NewRedisStreamQueue(redisClient, queue.RedisStreamConfig{
		Stream:   envOr("SOJ_JUDGE_RESULT_STREAM", cfg.Redis.Stream+":results"),
		Group:    envOr("SOJ_JUDGE_RESULT_GROUP", "judge-result-consumers"),
		Consumer: judgeAgentConsumerName(),
	})
	if err := requestQueue.Ensure(ctx); err != nil {
		return err
	}

	agent := submission.NewFakeAsyncAgent(submission.FakeAsyncAgentOptions{
		Judge:           newJudgeEngine(cfg.Judge),
		SourceStore:     submission.NewObjectSourceStore(objectStore),
		ResultPublisher: queueResultPublisher{queue: resultQueue},
	})

	router := httpapi.NewRouter(httpapi.RouterOptions{Metrics: metrics, ReadyCheck: func(ctx context.Context) error {
		return redisClient.Ping(ctx).Err()
	}})
	server := &http.Server{
		Addr:         *healthAddr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	logger.InfoContext(ctx, "starting soj judge agent", "health_addr", *healthAddr, "request_stream", envOr("SOJ_JUDGE_REQUEST_STREAM", cfg.Redis.Stream), "result_stream", envOr("SOJ_JUDGE_RESULT_STREAM", cfg.Redis.Stream+":results"))
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 2)
	go func() {
		errCh <- runHTTPServer(runCtx, server, cfg.Worker.ShutdownTimeout)
	}()
	go func() {
		errCh <- runJudgeAgentLoop(runCtx, agent, requestQueue, cfg.Redis.BatchSize, cfg.Redis.Block)
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

func runJudgeAgentLoop(ctx context.Context, agent *submission.FakeAsyncAgent, requestQueue queue.TaskQueue, batchSize int, block time.Duration) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		messages, err := requestQueue.Consume(ctx, batchSize, block)
		if err != nil {
			return err
		}
		for _, message := range messages {
			if err := agent.ProcessRequestMessage(ctx, message, requestQueue); err != nil {
				if deadErr := requestQueue.DeadLetter(ctx, message, err.Error()); deadErr != nil {
					return deadErr
				}
				if ackErr := requestQueue.Ack(ctx, message.ID); ackErr != nil {
					return ackErr
				}
			}
		}
	}
}

type queueResultPublisher struct {
	queue queue.TaskQueue
}

func (p queueResultPublisher) PublishResult(ctx context.Context, event judgeevents.ResultEvent) (string, error) {
	if err := event.Validate(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	return p.queue.Publish(ctx, 0, payload)
}

func newJudgeAgentObjectStorage(cfg config.StorageConfig) (storage.ObjectStorage, error) {
	return storage.NewS3Storage(storage.S3Options{
		Endpoint:        cfg.Endpoint,
		AccessKeyID:     cfg.AccessKey,
		SecretAccessKey: cfg.SecretKey,
		Bucket:          cfg.Bucket,
		Region:          cfg.Region,
		PathStyle:       cfg.UsePathStyle,
	})
}

func judgeAgentConsumerName() string {
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return hostname
	}
	return "judge-agent"
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
