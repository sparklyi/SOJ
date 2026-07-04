package config

import (
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
