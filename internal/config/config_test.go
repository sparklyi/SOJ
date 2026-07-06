package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadUsesEnvironmentOverrides(t *testing.T) {
	t.Setenv("SOJ_ENV", "test")
	t.Setenv("SOJ_HTTP_ADDR", ":19090")
	t.Setenv("SOJ_WORKER_HEALTH_ADDR", ":19091")
	t.Setenv("SOJ_DATABASE_DSN", "postgres://soj:soj@localhost:5432/soj?sslmode=disable")
	t.Setenv("SOJ_REDIS_ADDR", "localhost:6380")
	t.Setenv("SOJ_STORAGE_BUCKET", "soj-test")
	t.Setenv("SOJ_STORAGE_PATH_STYLE", "true")
	t.Setenv("SOJ_JUDGE_TIMEOUT", "12s")
	t.Setenv("SOJ_JWT_SECRET", "test-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Env != "test" {
		t.Fatalf("Env = %q, want test", cfg.Env)
	}
	if cfg.HTTP.Addr != ":19090" {
		t.Fatalf("HTTP.Addr = %q", cfg.HTTP.Addr)
	}
	if cfg.Worker.HealthAddr != ":19091" {
		t.Fatalf("Worker.HealthAddr = %q", cfg.Worker.HealthAddr)
	}
	if cfg.Database.DSN == "" {
		t.Fatal("Database.DSN was not loaded")
	}
	if cfg.Redis.Addr != "localhost:6380" {
		t.Fatalf("Redis.Addr = %q", cfg.Redis.Addr)
	}
	if cfg.Storage.Bucket != "soj-test" || !cfg.Storage.UsePathStyle {
		t.Fatalf("Storage config = %+v", cfg.Storage)
	}
	if cfg.Judge.Timeout != 12*time.Second {
		t.Fatalf("Judge.Timeout = %v", cfg.Judge.Timeout)
	}
	if cfg.Auth.JWTSecret != "test-secret" {
		t.Fatal("Auth.JWTSecret was not loaded from env")
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	t.Setenv("SOJ_JUDGE_TIMEOUT", "not-a-duration")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid duration error")
	}
}

func TestLoadDefaultsJudgeEndpointToAgentProtocol(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Judge.Endpoint != "agent://local" {
		t.Fatalf("Judge.Endpoint = %q, want agent://local", cfg.Judge.Endpoint)
	}
}

func TestLoadDefaultsTracingDisabledEvenWithOTELExporterEnv(t *testing.T) {
	t.Setenv("SOJ_TRACING_ENABLED", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Tracing.Enabled {
		t.Fatal("Tracing.Enabled = true, want default disabled")
	}
}

func TestLoadParsesTracingConfiguration(t *testing.T) {
	t.Setenv("SOJ_TRACING_ENABLED", "true")
	t.Setenv("OTEL_SERVICE_NAME", "custom-soj")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=test")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://collector:4318/v1/traces")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Tracing.Enabled {
		t.Fatal("Tracing.Enabled = false, want true")
	}
	if cfg.Tracing.ServiceName != "custom-soj" {
		t.Fatalf("Tracing.ServiceName = %q, want custom-soj", cfg.Tracing.ServiceName)
	}
	if cfg.Tracing.ResourceAttributes != "deployment.environment=test" {
		t.Fatalf("Tracing.ResourceAttributes = %q", cfg.Tracing.ResourceAttributes)
	}
	if cfg.Tracing.ExporterEndpoint != "http://collector:4318/v1/traces" {
		t.Fatalf("Tracing.ExporterEndpoint = %q", cfg.Tracing.ExporterEndpoint)
	}
}

func TestLoadRejectsInvalidTracingEnabled(t *testing.T) {
	t.Setenv("SOJ_TRACING_ENABLED", "definitely")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid tracing enabled error")
	}
	if got := err.Error(); !strings.HasPrefix(got, "SOJ_TRACING_ENABLED") {
		t.Fatalf("Load() error = %v, want SOJ_TRACING_ENABLED parse error", err)
	}
}
