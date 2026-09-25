package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"SOJ/internal/config"
	"SOJ/internal/httpapi"
	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/judgecore"
	"SOJ/internal/judgecore/sandbox"
	"SOJ/internal/language"
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
	configFlags := config.RegisterFlags(fs)
	healthAddr := fs.String("health-addr", "", "judge agent health HTTP listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if configFlags.Print {
		return config.Print(stdout, configFlags.File)
	}

	cfg, err := config.Load(config.Options{Role: config.RoleJudgeAgent, File: configFlags.File})
	if err != nil {
		return err
	}
	if *healthAddr != "" {
		cfg.Agent.HealthAddr = *healthAddr
	}
	sandboxBackend, err := sandbox.SelectBackend(cfg.Env, cfg.Judge.SandboxBackend, cfg.Judge.Endpoint)
	if err != nil {
		return err
	}
	catalog, err := loadLanguages(cfg)
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

	agentStreams, err := parseJudgeAgentStreams(cfg.Redis.AgentStreams)
	if err != nil {
		return err
	}
	requestQueues := judgeAgentRequestQueues(redisClient, cfg, agentStreams)
	if len(requestQueues) == 0 {
		return fmt.Errorf("redis.agent_streams=%q leaves the agent with no stream to consume", cfg.Redis.AgentStreams)
	}
	resultQueue := queue.NewRedisStreamQueue(redisClient, queue.RedisStreamConfig{
		Stream:     cfg.Redis.ResultStream,
		Group:      cfg.Redis.ResultGroup,
		Consumer:   judgeAgentConsumerName(),
		StartID:    "0",
		MaxLen:     cfg.Redis.StreamMaxLen,
		DeadMaxLen: cfg.Redis.DeadStreamMaxLen,
	})
	for _, stream := range requestQueues {
		if err := stream.Queue.Ensure(ctx); err != nil {
			return err
		}
	}
	if err := resultQueue.Ensure(ctx); err != nil {
		return err
	}

	languageSlots, err := parseJudgeAgentLanguageSlots(cfg.Judge.LanguageSlots)
	if err != nil {
		return err
	}
	slotLimiter := newJudgeAgentSlotLimiter(cfg.Judge.Parallelism, languageSlots)

	agent, sandboxReady, err := newJudgeAgentProcessor(ctx, sandboxBackend, cfg, catalog, objectStore, metrics, logger, queueResultPublisher{queue: resultQueue})
	if err != nil {
		return err
	}

	readiness := newJudgeAgentReadiness(requestQueues, resultQueue, objectStore, sandboxReady, metrics)
	router := httpapi.NewRouter(httpapi.RouterOptions{
		Metrics:        metrics,
		ReadyCheck:     readiness.Check,
		TracingEnabled: tracing.Enabled(),
		TracingService: tracing.ServiceName(),
	})
	server := &http.Server{
		Addr:         cfg.Agent.HealthAddr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	logger.InfoContext(ctx, "starting soj judge agent", "health_addr", cfg.Agent.HealthAddr, "request_streams", judgeRequestStreamNames(requestQueues), "result_stream", cfg.Redis.ResultStream, "sandbox_backend", sandboxBackend, "parallelism", cfg.Judge.Parallelism)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1+len(requestQueues))
	go func() {
		errCh <- runHTTPServer(runCtx, server, cfg.Worker.ShutdownTimeout)
	}()
	for _, stream := range requestQueues {
		go func(stream judgeRequestStream) {
			errCh <- runJudgeAgentLoop(runCtx, agent, stream.Queue, cfg.Judge.MaxBatch, cfg.Redis.Block, slotLimiter, metrics)
		}(stream)
	}

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
	ProcessRequestMessage(ctx context.Context, message queue.Message, requestQueue submission.MessageAcker) error
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
	return request.LanguageSlug
}

func newJudgeAgentProcessor(ctx context.Context, backend string, cfg config.Config, languages *language.Catalog, objectStore storage.ObjectStorage, metrics *observability.Metrics, logger *slog.Logger, publisher submission.ResultPublisher) (judgeRequestProcessor, observability.CheckFunc, error) {
	sourceStore := submission.NewObjectSourceStore(objectStore)
	if backend == sandbox.BackendFake {
		// One fake engine serves both capabilities; passing it twice keeps each
		// field honest about which operation it is used for.
		engine := fakeJudgeEngine(cfg.Judge.Endpoint)
		return submission.NewFakeAsyncAgent(submission.FakeAsyncAgentOptions{
			Judge:           engine,
			Run:             engine,
			SourceStore:     sourceStore,
			ResultPublisher: publisher,
		}), nil, nil
	}
	runtimeSandbox, err := newJudgeAgentSandbox(backend, cfg.Agent.Runner, probeImage(languages), cfg.Judge.CleanupTimeout, metrics, logger)
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
	testcaseCache := submission.NewTestcaseCache(objectStore, submission.TestcaseCacheOptions{Metrics: metrics})
	return submission.NewCoreAsyncAgent(submission.CoreAsyncAgentOptions{
		Core:            judgecore.New(judgecore.Options{Sandbox: runtimeSandbox, CleanupTimeout: cfg.Judge.CleanupTimeout}),
		Languages:       languages,
		SourceStore:     sourceStore,
		TestcaseLoader:  testcaseCache,
		ResultPublisher: publisher,
	}), sandboxReady, nil
}

func newJudgeAgentSandbox(backend string, runner config.RunnerConfig, probeImage string, cleanupTimeout time.Duration, observer sandbox.SandboxObserver, logger *slog.Logger) (sandbox.Sandbox, error) {
	switch backend {
	case sandbox.BackendProcess:
		return sandbox.NewProcessSandbox(), nil
	case sandbox.BackendDocker:
		return sandbox.NewDockerSandbox(sandbox.DockerSandboxOptions{
			ProbeImage:     probeImage,
			Runtime:        runner.Runtime,
			TempDir:        runner.Workdir,
			User:           runner.User,
			CleanupTimeout: cleanupTimeout,
			Observer:       observer,
			Logger:         logger,
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
