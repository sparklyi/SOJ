package app

import (
	"context"
	"flag"
	"io"
	"net/http"
	"strings"

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
	})

	middleware := httpapi.DefaultMiddlewareSet()
	middleware.Auth = actorMiddleware(jwtManager)
	router := httpapi.NewRouter(httpapi.RouterOptions{
		Middleware: middleware,
		ReadyCheck: pool.Ping,
		Modules: []httpapi.Module{
			user.NewModule(userService),
			problem.NewModule(problemService),
			contest.NewModule(contestService),
			submission.NewModule(submission.NewHandler(submissionService)),
		},
	})
	logger.InfoContext(ctx, "starting soj api", "addr", cfg.HTTP.Addr)

	server := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}
	return runHTTPServer(ctx, server, cfg.Worker.ShutdownTimeout)
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
