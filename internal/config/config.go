package config

import (
	"time"

	"SOJ/internal/judgecore/sandbox"
	"SOJ/internal/queue"
)

// Config is the runtime configuration.
//
// Defaults live in defaults. An optional YAML file overrides them, and ${VAR}
// references inside that file are filled from the environment, so values that
// must not be committed (secrets) or must differ per deployment can stay
// outside the file. The only environment variable the loader itself reads is
// SOJ_CONFIG_FILE, which names the file.
type Config struct {
	Env        string           `yaml:"env"`
	HTTP       HTTPConfig       `yaml:"http"`
	Worker     WorkerConfig     `yaml:"worker"`
	Database   DatabaseConfig   `yaml:"database"`
	Redis      RedisConfig      `yaml:"redis"`
	Storage    StorageConfig    `yaml:"storage"`
	Judge      JudgeConfig      `yaml:"judge"`
	Agent      AgentConfig      `yaml:"agent"`
	Retention  RetentionConfig  `yaml:"retention"`
	Auth       AuthConfig       `yaml:"auth"`
	Log        LogConfig        `yaml:"log"`
	Migrations MigrationsConfig `yaml:"migrations"`
	Tracing    TracingConfig    `yaml:"tracing"`
	// LanguagesDir holds one directory per language; see internal/language.
	LanguagesDir string `yaml:"languages_dir"`
}

type HTTPConfig struct {
	Addr         string        `yaml:"addr"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

type WorkerConfig struct {
	HealthAddr      string        `yaml:"health_addr"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

type DatabaseConfig struct {
	// DSN carries credentials, so it usually comes from the environment.
	DSN string `yaml:"dsn" secret:"true"`
}

type RedisConfig struct {
	Addr             string        `yaml:"addr"`
	Stream           string        `yaml:"stream"`
	Group            string        `yaml:"group"`
	BatchSize        int           `yaml:"batch_size"`
	Block            time.Duration `yaml:"block"`
	StreamMaxLen     int64         `yaml:"stream_max_len"`
	DeadStreamMaxLen int64         `yaml:"dead_stream_max_len"`

	// Self-runs travel on their own request stream so playground traffic
	// cannot queue in front of a formal submission. Empty values derive from
	// Stream at load time.
	RunStream    string `yaml:"run_stream"`
	RunGroup     string `yaml:"run_group"`
	ResultStream string `yaml:"result_stream"`
	ResultGroup  string `yaml:"result_group"`

	// AgentStreams selects which request streams a judge agent consumes:
	// "all", "submissions", or "runs". A second agent set to "runs" gets its
	// own capacity without touching submission latency.
	AgentStreams string `yaml:"agent_streams"`
	AgentGroup   string `yaml:"agent_group"`
}

type StorageConfig struct {
	Endpoint     string `yaml:"endpoint"`
	Bucket       string `yaml:"bucket"`
	Region       string `yaml:"region"`
	AccessKey    string `yaml:"access_key" secret:"true"`
	SecretKey    string `yaml:"secret_key" secret:"true"`
	UsePathStyle bool   `yaml:"path_style"`
}

type JudgeConfig struct {
	Endpoint string `yaml:"endpoint"`
	// SandboxBackend is fake, process, or docker. Only the judge agent may
	// select docker; the sandbox package rejects the combination otherwise.
	SandboxBackend string        `yaml:"sandbox_backend"`
	Timeout        time.Duration `yaml:"timeout"`
	CleanupTimeout time.Duration `yaml:"cleanup_timeout"`

	// Parallelism and LanguageSlots bound the sandbox slots of one judge
	// agent. MaxBatch bounds how many requests one loop iteration takes.
	Parallelism   int    `yaml:"parallelism"`
	LanguageSlots string `yaml:"language_slots"`
	MaxBatch      int    `yaml:"max_batch"`

	// RunParallelism caps runs executed inside the API process. It only
	// applies to the local:// endpoint; with agent:// the agent's own
	// parallelism is the limit and this process never executes anything.
	RunParallelism int `yaml:"run_parallelism"`
	// RunPerUser caps in-flight self-runs per user. The agent's capacity is
	// shared, so without this one caller can drain every slot.
	RunPerUser int `yaml:"run_per_user"`
	// RunStdinMaxBytes bounds the stdin a run may carry. It is stored in the
	// run row and placed on the request event, so it cannot be unbounded.
	RunStdinMaxBytes int `yaml:"run_stdin_max_bytes"`
}

// AgentConfig configures the judge agent process. The API, worker, and
// migration commands ignore it.
type AgentConfig struct {
	HealthAddr string       `yaml:"health_addr"`
	Runner     RunnerConfig `yaml:"runner"`
}

// RunnerConfig points the docker sandbox at its runner containers. The image
// for a language comes from that language's profile, not from here.
type RunnerConfig struct {
	Runtime string `yaml:"runtime"`
	Workdir string `yaml:"workdir"`
	User    string `yaml:"user"`
}

// RetentionConfig bounds how long the data a self-run leaves behind is kept.
//
// Self-runs are scratch work, so unlike a submission nothing about them has to
// survive -- but every one of them writes a source object to storage, and
// nothing else would ever remove it.
type RetentionConfig struct {
	// RunDays is how long a finished self-run and its source object are kept.
	// Zero disables the sweep; it is the default an operator should reach for
	// when they want retention off, so it must not read as "keep nothing".
	RunDays int `yaml:"run_days"`
	// RunInterval is how often the worker sweeps.
	RunInterval time.Duration `yaml:"run_interval"`
	// RunBatch bounds how many runs one sweep removes, so a backlog drains
	// over several sweeps instead of one long transaction.
	RunBatch int `yaml:"run_batch"`
}

type AuthConfig struct {
	JWTSecret       string        `yaml:"jwt_secret" secret:"true"`
	AccessTokenTTL  time.Duration `yaml:"access_token_ttl"`
	RefreshTokenTTL time.Duration `yaml:"refresh_token_ttl"`
}

type LogConfig struct {
	Level string `yaml:"level"`
}

type MigrationsConfig struct {
	Dir string `yaml:"dir"`
}

type TracingConfig struct {
	Enabled            bool   `yaml:"enabled"`
	ServiceName        string `yaml:"service_name"`
	ResourceAttributes string `yaml:"resource_attributes"`
	ExporterEndpoint   string `yaml:"exporter_endpoint"`
}

// defaults returns the configuration before any file or environment input.
func defaults() Config {
	return Config{
		Env: "dev",
		// Relative to the configuration file when one is loaded; ./languages
		// otherwise.
		LanguagesDir: "languages",
		HTTP: HTTPConfig{
			Addr:         ":8080",
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		Worker: WorkerConfig{
			HealthAddr:      ":8081",
			ShutdownTimeout: 10 * time.Second,
		},
		Redis: RedisConfig{
			Addr:             "localhost:6379",
			Stream:           "soj:judge:tasks",
			Group:            "judge-workers",
			BatchSize:        16,
			Block:            5 * time.Second,
			StreamMaxLen:     queue.DefaultStreamMaxLen,
			DeadStreamMaxLen: queue.DefaultDeadStreamMaxLen,
			RunGroup:         "judge-run-agents",
			ResultGroup:      "judge-result-consumers",
			AgentStreams:     AgentStreamsAll,
			AgentGroup:       "judge-agents",
		},
		Storage: StorageConfig{
			Endpoint: "http://localhost:9000",
			Bucket:   "soj",
			Region:   "us-east-1",
		},
		Judge: JudgeConfig{
			Endpoint:         "agent://local",
			Timeout:          30 * time.Second,
			CleanupTimeout:   sandbox.DefaultCleanupTimeout,
			Parallelism:      1,
			RunParallelism:   1,
			RunPerUser:       2,
			RunStdinMaxBytes: 64 << 10,
		},
		Agent: AgentConfig{
			HealthAddr: ":8082",
		},
		Retention: RetentionConfig{
			RunDays:     7,
			RunInterval: 10 * time.Minute,
			RunBatch:    200,
		},
		Auth: AuthConfig{
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 30 * 24 * time.Hour,
		},
		Log: LogConfig{
			Level: "info",
		},
		Migrations: MigrationsConfig{
			Dir: "internal/migrations",
		},
	}
}

// normalize derives values that depend on other values. It runs after the file
// is applied and before validation, so the rest of the program only sees a
// complete configuration.
func (c *Config) normalize() {
	if c.Redis.RunStream == "" {
		c.Redis.RunStream = c.Redis.Stream + ":runs"
	}
	if c.Redis.ResultStream == "" {
		c.Redis.ResultStream = c.Redis.Stream + ":results"
	}
	if c.Judge.MaxBatch == 0 {
		c.Judge.MaxBatch = c.Redis.BatchSize
	}
}
