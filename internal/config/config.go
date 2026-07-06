package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Env        string
	HTTP       HTTPConfig
	Worker     WorkerConfig
	Database   DatabaseConfig
	Redis      RedisConfig
	Storage    StorageConfig
	Judge      JudgeConfig
	Auth       AuthConfig
	Log        LogConfig
	Migrations MigrationsConfig
	Tracing    TracingConfig
}

type HTTPConfig struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type WorkerConfig struct {
	HealthAddr      string
	ShutdownTimeout time.Duration
}

type DatabaseConfig struct {
	DSN string
}

type RedisConfig struct {
	Addr      string
	Stream    string
	Group     string
	BatchSize int
	Block     time.Duration
}

type StorageConfig struct {
	Endpoint     string
	Bucket       string
	Region       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
}

type JudgeConfig struct {
	Endpoint string
	Timeout  time.Duration
}

type AuthConfig struct {
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

type LogConfig struct {
	Level string
}

type MigrationsConfig struct {
	Dir string
}

type TracingConfig struct {
	Enabled            bool
	ServiceName        string
	ResourceAttributes string
	ExporterEndpoint   string
}

func Load() (Config, error) {
	cfg := Config{
		Env: env("SOJ_ENV", "dev"),
		HTTP: HTTPConfig{
			Addr:         env("SOJ_HTTP_ADDR", ":8080"),
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		Worker: WorkerConfig{
			HealthAddr:      env("SOJ_WORKER_HEALTH_ADDR", ":8081"),
			ShutdownTimeout: 10 * time.Second,
		},
		Database: DatabaseConfig{
			DSN: env("SOJ_DATABASE_DSN", ""),
		},
		Redis: RedisConfig{
			Addr:      env("SOJ_REDIS_ADDR", "localhost:6379"),
			Stream:    env("SOJ_REDIS_STREAM", "soj:judge:tasks"),
			Group:     env("SOJ_REDIS_GROUP", "judge-workers"),
			BatchSize: 16,
			Block:     5 * time.Second,
		},
		Storage: StorageConfig{
			Endpoint:  env("SOJ_STORAGE_ENDPOINT", "http://localhost:9000"),
			Bucket:    env("SOJ_STORAGE_BUCKET", "soj"),
			Region:    env("SOJ_STORAGE_REGION", "us-east-1"),
			AccessKey: env("SOJ_STORAGE_ACCESS_KEY", ""),
			SecretKey: env("SOJ_STORAGE_SECRET_KEY", ""),
		},
		Judge: JudgeConfig{
			Endpoint: env("SOJ_JUDGE_ENDPOINT", "agent://local"),
			Timeout:  30 * time.Second,
		},
		Auth: AuthConfig{
			JWTSecret:       env("SOJ_JWT_SECRET", ""),
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 30 * 24 * time.Hour,
		},
		Log: LogConfig{
			Level: env("SOJ_LOG_LEVEL", "info"),
		},
		Migrations: MigrationsConfig{
			Dir: env("SOJ_MIGRATIONS_DIR", "internal/migrations"),
		},
		Tracing: TracingConfig{
			ServiceName:        env("OTEL_SERVICE_NAME", ""),
			ResourceAttributes: env("OTEL_RESOURCE_ATTRIBUTES", ""),
			ExporterEndpoint:   tracingExporterEndpoint(),
		},
	}

	var err error
	if cfg.HTTP.ReadTimeout, err = envDuration("SOJ_HTTP_READ_TIMEOUT", cfg.HTTP.ReadTimeout); err != nil {
		return Config{}, err
	}
	if cfg.HTTP.WriteTimeout, err = envDuration("SOJ_HTTP_WRITE_TIMEOUT", cfg.HTTP.WriteTimeout); err != nil {
		return Config{}, err
	}
	if cfg.Worker.ShutdownTimeout, err = envDuration("SOJ_SHUTDOWN_TIMEOUT", cfg.Worker.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if cfg.Redis.BatchSize, err = envInt("SOJ_REDIS_BATCH_SIZE", cfg.Redis.BatchSize); err != nil {
		return Config{}, err
	}
	if cfg.Redis.Block, err = envDuration("SOJ_REDIS_BLOCK", cfg.Redis.Block); err != nil {
		return Config{}, err
	}
	if cfg.Storage.UsePathStyle, err = envBool("SOJ_STORAGE_PATH_STYLE", false); err != nil {
		return Config{}, err
	}
	if cfg.Judge.Timeout, err = envDuration("SOJ_JUDGE_TIMEOUT", cfg.Judge.Timeout); err != nil {
		return Config{}, err
	}
	if cfg.Auth.AccessTokenTTL, err = envDuration("SOJ_ACCESS_TOKEN_TTL", cfg.Auth.AccessTokenTTL); err != nil {
		return Config{}, err
	}
	if cfg.Auth.RefreshTokenTTL, err = envDuration("SOJ_REFRESH_TOKEN_TTL", cfg.Auth.RefreshTokenTTL); err != nil {
		return Config{}, err
	}
	if cfg.Tracing.Enabled, err = envBool("SOJ_TRACING_ENABLED", false); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func tracingExporterEndpoint() string {
	if value := env("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", ""); value != "" {
		return value
	}
	return env("OTEL_EXPORTER_OTLP_ENDPOINT", "")
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func envInt(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func envBool(key string, fallback bool) (bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}
