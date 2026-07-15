package app

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	tracing, err := setupProcessTracing(ctx, cfg, "soj-judge-agent")
	if err != nil {
		return err
	}
	defer shutdownProcessTracing(ctx, cfg.Worker.ShutdownTimeout, logger, tracing)
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
		StartID:  "0",
	})
	if err := requestQueue.Ensure(ctx); err != nil {
		return err
	}
	if err := resultQueue.Ensure(ctx); err != nil {
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

	agent, sandboxReady, err := newJudgeAgentProcessor(ctx, sandboxBackend, cfg, objectStore, metrics, logger, queueResultPublisher{queue: resultQueue})
	if err != nil {
		return err
	}

	readiness := newJudgeAgentReadiness(requestQueue, resultQueue, objectStore, sandboxReady, metrics)
	router := httpapi.NewRouter(httpapi.RouterOptions{
		Metrics:        metrics,
		ReadyCheck:     readiness.Check,
		TracingEnabled: tracing.Enabled(),
		TracingService: tracing.ServiceName(),
	})
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
		errCh <- runJudgeAgentLoop(runCtx, agent, requestQueue, maxBatch, cfg.Redis.Block, slotLimiter, metrics)
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

type judgeAgentSlotMetrics interface {
	ObserveJudgeAgentSlots(scope, language string, used, capacity int)
}

func runJudgeAgentLoop(ctx context.Context, agent judgeRequestProcessor, requestQueue queue.TaskQueue, batchSize int, block time.Duration, slots *judgeAgentSlotLimiter, metrics judgeAgentSlotMetrics) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	errCh := make(chan error, 1)
	recordJudgeAgentSlotMetrics(metrics, slots)
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
		consumeCtx, consumeSpan := observability.Tracer("SOJ/internal/app").Start(ctx, "judge_agent.request.consume", trace.WithAttributes(attribute.Int("soj.judge_agent.limit", limit)))
		messages, err := requestQueue.Consume(consumeCtx, limit, block)
		consumeSpan.SetAttributes(attribute.Int("soj.judge_agent.messages", len(messages)))
		if err != nil {
			consumeSpan.SetStatus(codes.Error, "consume_error")
			consumeSpan.End()
			return err
		}
		consumeSpan.End()
		for _, message := range messages {
			language := judgeAgentMessageLanguageKey(message)
			acquireCtx, acquireSpan := observability.Tracer("SOJ/internal/app").Start(ctx, "judge_agent.slot.acquire", trace.WithAttributes(attribute.String("soj.language", language)))
			release, err := slots.Acquire(acquireCtx, language)
			if err != nil {
				acquireSpan.SetStatus(codes.Error, "slot_acquire_error")
				acquireSpan.End()
				return err
			}
			acquireSpan.End()
			recordJudgeAgentSlotMetrics(metrics, slots)
			wg.Add(1)
			go func(message queue.Message) {
				defer wg.Done()
				defer func() {
					release()
					recordJudgeAgentSlotMetrics(metrics, slots)
				}()
				processCtx, span := observability.Tracer("SOJ/internal/app").Start(ctx, "judge_agent.request.process")
				if err := agent.ProcessRequestMessage(processCtx, message, requestQueue); err != nil {
					span.SetStatus(codes.Error, "process_error")
					span.End()
					if deadErr := requestQueue.DeadLetter(ctx, message, err.Error()); deadErr != nil {
						sendJudgeAgentLoopError(errCh, deadErr)
						return
					}
					if ackErr := requestQueue.Ack(ctx, message.ID); ackErr != nil {
						sendJudgeAgentLoopError(errCh, ackErr)
						return
					}
					return
				}
				span.End()
			}(message)
		}
	}
}

func recordJudgeAgentSlotMetrics(metrics judgeAgentSlotMetrics, slots *judgeAgentSlotLimiter) {
	if metrics == nil || slots == nil {
		return
	}
	for _, usage := range slots.Usages() {
		metrics.ObserveJudgeAgentSlots(usage.Scope, usage.Language, usage.Used, usage.Capacity)
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

func newJudgeAgentProcessor(ctx context.Context, backend string, cfg config.Config, objectStore storage.ObjectStorage, observer sandbox.SandboxObserver, logger *slog.Logger, publisher submission.ResultPublisher) (judgeRequestProcessor, observability.CheckFunc, error) {
	sourceStore := submission.NewObjectSourceStore(objectStore)
	if backend == sandbox.BackendFake {
		return submission.NewFakeAsyncAgent(submission.FakeAsyncAgentOptions{
			Judge:           newJudgeEngine(cfg.Judge),
			SourceStore:     sourceStore,
			ResultPublisher: publisher,
		}), nil, nil
	}
	runtimeSandbox, err := newJudgeAgentSandbox(backend, cfg.Judge.CleanupTimeout, observer, logger)
	if err != nil {
		return nil, nil, err
	}
	capabilities, err := runtimeSandbox.Probe(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := sandbox.ValidateProductionCapabilities(cfg.Env, capabilities); err != nil {
		return nil, nil, err
	}
	sandboxReady := func(ctx context.Context) error {
		capabilities, err := runtimeSandbox.Probe(ctx)
		if err != nil {
			return err
		}
		return sandbox.ValidateProductionCapabilities(cfg.Env, capabilities)
	}
	return submission.NewCoreAsyncAgent(submission.CoreAsyncAgentOptions{
		Core:            judgecore.New(judgecore.Options{Sandbox: runtimeSandbox, CleanupTimeout: cfg.Judge.CleanupTimeout}),
		SourceStore:     sourceStore,
		ResultPublisher: publisher,
	}), sandboxReady, nil
}

func newJudgeAgentSandbox(backend string, cleanupTimeout time.Duration, observer sandbox.SandboxObserver, logger *slog.Logger) (sandbox.Sandbox, error) {
	switch backend {
	case sandbox.BackendProcess:
		return sandbox.NewProcessSandbox(), nil
	case sandbox.BackendDocker:
		return sandbox.NewDockerSandbox(sandbox.DockerSandboxOptions{
			Runtime:        envOr("SOJ_DOCKER_RUNNER_RUNTIME", ""),
			TempDir:        envOr("SOJ_DOCKER_RUNNER_WORKDIR", ""),
			User:           envOr("SOJ_DOCKER_RUNNER_USER", ""),
			CleanupTimeout: cleanupTimeout,
			Observer:       observer,
			Logger:         logger,
			Images: map[string]string{
				"go":    envOr("SOJ_DOCKER_RUNNER_IMAGE_GO", "ghcr.io/sparklyi/soj-runner-go:main"),
				"cpp17": envOr("SOJ_DOCKER_RUNNER_IMAGE_CPP17", "ghcr.io/sparklyi/soj-runner-cpp17:main"),
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
