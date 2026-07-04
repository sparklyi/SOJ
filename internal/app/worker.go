package app

import (
	"context"
	"flag"
	"io"
	"net/http"

	"SOJ/internal/config"
	"SOJ/internal/httpapi"
	"SOJ/internal/observability"
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
	router := httpapi.NewRouter(httpapi.RouterOptions{})
	logger.InfoContext(ctx, "starting soj worker health server", "addr", cfg.Worker.HealthAddr)

	server := &http.Server{
		Addr:         cfg.Worker.HealthAddr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}
	return runHTTPServer(ctx, server, cfg.Worker.ShutdownTimeout)
}
