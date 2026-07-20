package app

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"SOJ/internal/auth"
	"SOJ/internal/config"
	"SOJ/internal/contest"
	"SOJ/internal/httpapi"
	"SOJ/internal/observability"
	"SOJ/internal/postgres"
	"SOJ/internal/postgres/db"
	"SOJ/internal/problem"
	"SOJ/internal/storage"
	"SOJ/internal/submission"
	"SOJ/internal/user"

	"github.com/gin-gonic/gin"
)

func RunAPI(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("soj-api", flag.ContinueOnError)
	fs.SetOutput(stdout)
	addr := fs.String("addr", "", "HTTP listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *addr != "" {
		cfg.HTTP.Addr = *addr
	}

	logger := observability.NewLogger(cfg.Log.Level, stdout)
	tracing, err := setupProcessTracing(ctx, cfg, "soj-api")
	if err != nil {
		return err
	}
	defer shutdownProcessTracing(ctx, cfg.Worker.ShutdownTimeout, logger, tracing)
	metrics := observability.NewMetrics("soj-api")

	pool, err := postgres.OpenPool(ctx, postgres.PoolConfig{DSN: cfg.Database.DSN})
	if err != nil {
		return err
	}
	defer pool.Close()

	objectStorage, err := storage.NewS3Storage(storage.S3Options{
		Endpoint:        cfg.Storage.Endpoint,
		AccessKeyID:     cfg.Storage.AccessKey,
		SecretAccessKey: cfg.Storage.SecretKey,
		Bucket:          cfg.Storage.Bucket,
		Region:          cfg.Storage.Region,
		PathStyle:       cfg.Storage.UsePathStyle,
	})
	if err != nil {
		return err
	}

	jwtManager := auth.NewJWTManager(cfg.Auth.JWTSecret, cfg.Auth.AccessTokenTTL)
	queries := db.New(pool)
	userRepo := user.NewPostgresRepository(queries)
	userService := user.NewService(userRepo, jwtManager, user.WithTokenTTLs(cfg.Auth.AccessTokenTTL, cfg.Auth.RefreshTokenTTL))
	problemRepo := problem.NewPostgresRepository(pool)
	problemService := problem.NewService(problemRepo, objectStorage)
	contestRepo := contest.NewPostgresRepository(pool)
	contestService := contest.NewService(contestRepo)
	submissionRepo := submission.NewSQLRepositoryWithTxRunner(queries, pool)
	testcaseResolver := submission.NewTestcaseSnapshotResolver(queries, objectStorage)
	submissionService := submission.NewService(submission.ServiceOptions{
		Repository:       submissionRepo,
		ProblemReader:    problemService,
		TestcaseResolver: testcaseResolver,
		SourceStore:      submission.NewObjectSourceStore(objectStorage),
		Judge:            newJudgeEngine(cfg.Judge),
		ContestPolicy:    contestService,
		TerminalHook:     contestService,
		RunContext:       ctx,
		RunParallelism:   cfg.Judge.RunParallelism,
		RunTimeout:       cfg.Judge.Timeout,
	})
	rejudgeService := submission.NewRejudgeService(submissionRepo, rejudgeAuthorizationPolicy{problems: problemService, contests: contestService}, nil, metrics)

	middleware := httpapi.DefaultMiddlewareSet()
	middleware.Auth = actorMiddleware(jwtManager)
	router := httpapi.NewRouter(httpapi.RouterOptions{
		Middleware:     middleware,
		ReadyCheck:     pool.Ping,
		Metrics:        metrics,
		TracingEnabled: tracing.Enabled(),
		TracingService: tracing.ServiceName(),
		Modules: []httpapi.Module{
			user.NewModule(userService),
			problem.NewModule(problemService),
			contest.NewModule(contestService),
			submission.NewModule(submission.NewHandler(submissionService, rejudgeService)),
		},
	})
	logger.InfoContext(ctx, "starting soj api", "addr", cfg.HTTP.Addr)

	server := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}
	runErr := runHTTPServer(ctx, server, cfg.Worker.ShutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Worker.ShutdownTimeout)
	defer cancel()
	if closeErr := submissionService.Close(shutdownCtx); closeErr != nil {
		if runErr == nil {
			return closeErr
		}
		logger.ErrorContext(context.Background(), "shutdown submission run executions", "error", closeErr)
	}
	return runErr
}

type rejudgeAuthorizationPolicy struct {
	problems *problem.Service
	contests *contest.Service
}

func (p rejudgeAuthorizationPolicy) AuthorizeProblemRejudge(ctx context.Context, actor auth.Actor, problemID int64) error {
	return p.problems.AuthorizeProblemRejudge(ctx, actor, problemID)
}

func (p rejudgeAuthorizationPolicy) AuthorizeContestRejudge(ctx context.Context, actor auth.Actor, contestID int64) error {
	return p.contests.AuthorizeContestRejudge(ctx, actor, contestID)
}

func (p rejudgeAuthorizationPolicy) ValidateContestRejudgeTarget(ctx context.Context, contestID int64) error {
	return p.contests.ValidateContestRejudgeTarget(ctx, contestID)
}

func actorMiddleware(jwtManager *auth.JWTManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetString(httpapi.ContextRequestID)
		actor := auth.Anonymous(requestID)
		header := c.GetHeader("Authorization")
		if token, ok := bearerToken(header); ok {
			if parsed, err := jwtManager.ParseAccessToken(token); err == nil {
				parsed.RequestID = requestID
				actor = parsed
			}
		}
		c.Set(user.ActorContextKey, actor)
		c.Set("auth.actor", actor)
		c.Next()
	}
}

func bearerToken(header string) (string, bool) {
	const prefix = "bearer "
	header = strings.TrimSpace(header)
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}

func setupProcessTracing(ctx context.Context, cfg config.Config, defaultServiceName string) (observability.Tracing, error) {
	serviceName := strings.TrimSpace(cfg.Tracing.ServiceName)
	if serviceName == "" {
		serviceName = defaultServiceName
	}
	return observability.SetupTracing(ctx, observability.TracingOptions{
		Enabled:            cfg.Tracing.Enabled,
		ServiceName:        serviceName,
		ResourceAttributes: cfg.Tracing.ResourceAttributes,
		ExporterEndpoint:   cfg.Tracing.ExporterEndpoint,
	})
}

func shutdownProcessTracing(ctx context.Context, timeout time.Duration, logger *slog.Logger, tracing observability.Tracing) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := tracing.Shutdown(shutdownCtx); err != nil && logger != nil {
		logger.WarnContext(ctx, "failed to shut down tracing", "error", err)
	}
}
