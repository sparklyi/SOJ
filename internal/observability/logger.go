package observability

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

func NewLogger(level string, output io.Writer) *slog.Logger {
	if output == nil {
		output = os.Stdout
	}

	var slogLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn", "warning":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slogLevel}))
}
