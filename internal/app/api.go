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
	router := httpapi.NewRouter(httpapi.RouterOptions{})
	logger.InfoContext(ctx, "starting soj api", "addr", cfg.HTTP.Addr)

	server := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}
	return runHTTPServer(ctx, server, cfg.Worker.ShutdownTimeout)
}
