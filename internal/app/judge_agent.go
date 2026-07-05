package app

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"SOJ/internal/config"
	"SOJ/internal/httpapi"
	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/judgecore"
	"SOJ/internal/judgecore/sandbox"
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
	sandboxBackend, err := sandbox.SelectBackend(cfg.Env, envOr("SOJ_JUDGE_SANDBOX_BACKEND", ""), cfg.Judge.Endpoint)
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

	parallelism, err := envInt("SOJ_JUDGE_PARALLELISM", 1)
	if err != nil {
		return err
	}
	languageSlots, err := parseJudgeAgentLanguageSlots(envOr("SOJ_JUDGE_LANGUAGE_SLOTS", ""))
	if err != nil {
		return err
	}
	maxBatch, err := envInt("SOJ_JUDGE_MAX_BATCH", cfg.Redis.BatchSize)
	if err != nil {
		return err
	}
	slotLimiter := newJudgeAgentSlotLimiter(parallelism, languageSlots)

	agent, err := newJudgeAgentProcessor(ctx, sandboxBackend, cfg, objectStore, queueResultPublisher{queue: resultQueue})
	if err != nil {
		return err
	}

	router := httpapi.NewRouter(httpapi.RouterOptions{Metrics: metrics, ReadyCheck: func(ctx context.Context) error {
		return redisClient.Ping(ctx).Err()
	}})
	server := &http.Server{
		Addr:         *healthAddr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	logger.InfoContext(ctx, "starting soj judge agent", "health_addr", *healthAddr, "request_stream", envOr("SOJ_JUDGE_REQUEST_STREAM", cfg.Redis.Stream), "result_stream", envOr("SOJ_JUDGE_RESULT_STREAM", cfg.Redis.Stream+":results"), "sandbox_backend", sandboxBackend, "parallelism", parallelism)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 2)
	go func() {
		errCh <- runHTTPServer(runCtx, server, cfg.Worker.ShutdownTimeout)
	}()
	go func() {
		errCh <- runJudgeAgentLoop(runCtx, agent, requestQueue, maxBatch, cfg.Redis.Block, slotLimiter)
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

type judgeRequestProcessor interface {
	ProcessRequestMessage(ctx context.Context, message queue.Message, requestQueue queue.TaskQueue) error
}

func runJudgeAgentLoop(ctx context.Context, agent judgeRequestProcessor, requestQueue queue.TaskQueue, batchSize int, block time.Duration, slots *judgeAgentSlotLimiter) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	errCh := make(chan error, 1)
	for {
		select {
		case err := <-errCh:
			return err
		default:
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		available := slots.Available()
		if available <= 0 {
			select {
			case err := <-errCh:
				return err
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
				continue
			}
		}
		limit := batchSize
		if limit <= 0 || limit > available {
			limit = available
		}
		messages, err := requestQueue.Consume(ctx, limit, block)
		if err != nil {
			return err
		}
		for _, message := range messages {
			release, err := slots.Acquire(ctx, judgeAgentMessageLanguageKey(message))
			if err != nil {
				return err
			}
			wg.Add(1)
			go func(message queue.Message) {
				defer wg.Done()
				defer release()
				if err := agent.ProcessRequestMessage(ctx, message, requestQueue); err != nil {
					if deadErr := requestQueue.DeadLetter(ctx, message, err.Error()); deadErr != nil {
						sendJudgeAgentLoopError(errCh, deadErr)
						return
					}
					if ackErr := requestQueue.Ack(ctx, message.ID); ackErr != nil {
						sendJudgeAgentLoopError(errCh, ackErr)
						return
					}
				}
			}(message)
		}
	}
}

func sendJudgeAgentLoopError(errCh chan<- error, err error) {
	select {
	case errCh <- err:
	default:
	}
}

func judgeAgentMessageLanguageKey(message queue.Message) string {
	var request judgeevents.RequestEvent
	if err := json.Unmarshal(message.Payload, &request); err != nil {
		return ""
	}
	if request.LanguageSlug != "" {
		return request.LanguageSlug
	}
	if request.LanguageID != 0 {
		return strconv.FormatInt(request.LanguageID, 10)
	}
	return ""
}

func newJudgeAgentProcessor(ctx context.Context, backend string, cfg config.Config, objectStore storage.ObjectStorage, publisher submission.ResultPublisher) (judgeRequestProcessor, error) {
	sourceStore := submission.NewObjectSourceStore(objectStore)
	if backend == sandbox.BackendFake {
		return submission.NewFakeAsyncAgent(submission.FakeAsyncAgentOptions{
			Judge:           newJudgeEngine(cfg.Judge),
			SourceStore:     sourceStore,
			ResultPublisher: publisher,
		}), nil
	}
	sandboxBackend, err := newJudgeAgentSandbox(backend)
	if err != nil {
		return nil, err
	}
	capabilities, err := sandboxBackend.Probe(ctx)
	if err != nil {
		return nil, err
	}
	if err := sandbox.ValidateProductionCapabilities(cfg.Env, capabilities); err != nil {
		return nil, err
	}
	return submission.NewCoreAsyncAgent(submission.CoreAsyncAgentOptions{
		Core:            judgecore.New(judgecore.Options{Sandbox: sandboxBackend}),
		SourceStore:     sourceStore,
		ResultPublisher: publisher,
	}), nil
}

func newJudgeAgentSandbox(backend string) (sandbox.Sandbox, error) {
	switch backend {
	case sandbox.BackendProcess:
		return sandbox.NewProcessSandbox(), nil
	case sandbox.BackendDocker:
		return sandbox.NewDockerSandbox(sandbox.DockerSandboxOptions{
			Runtime: envOr("SOJ_DOCKER_RUNNER_RUNTIME", ""),
			Images: map[string]string{
				"go":    envOr("SOJ_DOCKER_RUNNER_IMAGE_GO", "soj-runner-go:local"),
				"cpp17": envOr("SOJ_DOCKER_RUNNER_IMAGE_CPP17", "soj-runner-cpp17:local"),
			},
		}), nil
	case sandbox.BackendIsolate:
		return nil, errIsolateSandboxUnavailable{}
	default:
		return nil, errUnsupportedSandboxBackend(backend)
	}
}

type errIsolateSandboxUnavailable struct{}

func (errIsolateSandboxUnavailable) Error() string {
	return "isolate sandbox execution is not implemented in this build"
}

type errUnsupportedSandboxBackend string

func (e errUnsupportedSandboxBackend) Error() string {
	return "unsupported judge-agent sandbox backend " + string(e)
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

func envInt(key string, fallback int) (int, error) {
	raw := envOr(key, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return value, nil
}
