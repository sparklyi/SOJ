package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SOJ/internal/queue"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func loadAgent(t *testing.T, contents string) Config {
	t.Helper()

	cfg, err := Load(Options{Role: RoleJudgeAgent, File: writeConfig(t, contents)})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return cfg
}

func TestLoadDefaultsWithoutFile(t *testing.T) {
	cfg, err := Load(Options{Role: RoleJudgeAgent})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Env != "dev" {
		t.Fatalf("Env = %q, want dev", cfg.Env)
	}
	if cfg.Judge.Endpoint != "agent://local" {
		t.Fatalf("Judge.Endpoint = %q, want agent://local", cfg.Judge.Endpoint)
	}
	if cfg.Judge.RunParallelism != 1 || cfg.Judge.RunPerUser != 2 {
		t.Fatalf("Judge runs = %+v", cfg.Judge)
	}
	if cfg.Redis.StreamMaxLen != queue.DefaultStreamMaxLen {
		t.Fatalf("Redis.StreamMaxLen = %d, want %d", cfg.Redis.StreamMaxLen, queue.DefaultStreamMaxLen)
	}
	if cfg.Redis.DeadStreamMaxLen != queue.DefaultDeadStreamMaxLen {
		t.Fatalf("Redis.DeadStreamMaxLen = %d, want %d", cfg.Redis.DeadStreamMaxLen, queue.DefaultDeadStreamMaxLen)
	}
	if cfg.Tracing.Enabled {
		t.Fatal("Tracing.Enabled = true, want default disabled")
	}
	if cfg.Redis.RunStream != "soj:judge:tasks:runs" {
		t.Fatalf("Redis.RunStream = %q, want derived stream", cfg.Redis.RunStream)
	}
	if cfg.Redis.ResultStream != "soj:judge:tasks:results" {
		t.Fatalf("Redis.ResultStream = %q, want derived stream", cfg.Redis.ResultStream)
	}
}

func TestLoadFileOverridesDefaults(t *testing.T) {
	cfg := loadAgent(t, `
env: docker
http:
  addr: ":19090"
  read_timeout: 3s
redis:
  addr: redis:6379
  run_stream: custom:runs
judge:
  timeout: 12s
  cleanup_timeout: 7s
  run_parallelism: 3
  language_slots: go=2
retention:
  run_days: 0
`)

	if cfg.Env != "docker" {
		t.Fatalf("Env = %q, want docker", cfg.Env)
	}
	if cfg.HTTP.Addr != ":19090" || cfg.HTTP.ReadTimeout != 3*time.Second {
		t.Fatalf("HTTP = %+v", cfg.HTTP)
	}
	if cfg.Redis.Addr != "redis:6379" || cfg.Redis.RunStream != "custom:runs" {
		t.Fatalf("Redis = %+v", cfg.Redis)
	}
	if cfg.Judge.Timeout != 12*time.Second || cfg.Judge.CleanupTimeout != 7*time.Second {
		t.Fatalf("Judge timeouts = %+v", cfg.Judge)
	}
	if cfg.Judge.RunParallelism != 3 || cfg.Judge.LanguageSlots != "go=2" {
		t.Fatalf("Judge runs = %+v", cfg.Judge)
	}
	if cfg.Retention.RunDays != 0 {
		t.Fatalf("Retention.RunDays = %d, want 0", cfg.Retention.RunDays)
	}
	if cfg.Judge.MaxBatch != cfg.Redis.BatchSize {
		t.Fatalf("Judge.MaxBatch = %d, want redis.batch_size %d", cfg.Judge.MaxBatch, cfg.Redis.BatchSize)
	}
}

func TestLoadFileRejectsUnknownField(t *testing.T) {
	_, err := Load(Options{Role: RoleJudgeAgent, File: writeConfig(t, "judge:\n  typo_field: 1\n")})
	if err == nil {
		t.Fatal("Load() error = nil, want unknown field error")
	}
	if !strings.Contains(err.Error(), "typo_field") {
		t.Fatalf("Load() error = %v, want the unknown field named", err)
	}
}

func TestLoadFileExpandsEnvironment(t *testing.T) {
	t.Setenv("SOJ_TEST_DSN", "postgres://example")

	cfg, err := Load(Options{Role: RoleAPI, File: writeConfig(t, `
database:
  dsn: ${SOJ_TEST_DSN}
auth:
  jwt_secret: ${SOJ_TEST_JWT:-dev-secret}
`)})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Database.DSN != "postgres://example" {
		t.Fatalf("Database.DSN = %q", cfg.Database.DSN)
	}
	if cfg.Auth.JWTSecret != "dev-secret" {
		t.Fatalf("Auth.JWTSecret = %q, want default", cfg.Auth.JWTSecret)
	}
}

func TestLoadFileReportsMissingEnvironment(t *testing.T) {
	_, err := Load(Options{Role: RoleJudgeAgent, File: writeConfig(t, "storage:\n  endpoint: ${SOJ_TEST_MISSING}\n")})
	if err == nil {
		t.Fatal("Load() error = nil, want missing variable error")
	}
	if !strings.Contains(err.Error(), "SOJ_TEST_MISSING") {
		t.Fatalf("Load() error = %v, want the missing variable named", err)
	}
}

func TestLoadRequiresRoleSecrets(t *testing.T) {
	file := writeConfig(t, "judge:\n  sandbox_backend: fake\n")

	if _, err := Load(Options{Role: RoleAPI, File: file}); err == nil {
		t.Fatal("Load() for api error = nil, want missing dsn and jwt secret")
	} else {
		if !strings.Contains(err.Error(), "database.dsn") || !strings.Contains(err.Error(), "auth.jwt_secret") {
			t.Fatalf("Load() error = %v, want both missing values reported", err)
		}
	}

	if _, err := Load(Options{Role: RoleWorker, File: file}); err == nil {
		t.Fatal("Load() for worker error = nil, want missing dsn")
	}

	if _, err := Load(Options{Role: RoleJudgeAgent, File: file}); err != nil {
		t.Fatalf("Load() for judge agent error = %v, want no database requirement", err)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     string
	}{
		{"unknown sandbox backend", "judge:\n  sandbox_backend: nonsense\n", "judge.sandbox_backend"},
		{"unknown agent streams", "redis:\n  agent_streams: sometimes\n", "redis.agent_streams"},
		{"negative retention", "retention:\n  run_days: -1\n", "retention.run_days"},
		{"negative batch", "judge:\n  max_batch: -1\n", "judge.max_batch"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(Options{Role: RoleJudgeAgent, File: writeConfig(t, test.contents)})
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load() error = %v, want %s", err, test.want)
			}
		})
	}
}

func TestLoadReportsMissingExplicitFile(t *testing.T) {
	_, err := Load(Options{Role: RoleJudgeAgent, File: filepath.Join(t.TempDir(), "absent.yaml")})
	if err == nil {
		t.Fatal("Load() error = nil, want read error")
	}
}

func TestPrintMasksSecrets(t *testing.T) {
	t.Setenv("SOJ_TEST_DSN", "postgres://user:password@db:5432/soj")
	t.Setenv("SOJ_TEST_JWT", "super-secret")

	file := writeConfig(t, `
env: prod
database:
  dsn: ${SOJ_TEST_DSN}
auth:
  jwt_secret: ${SOJ_TEST_JWT}
storage:
  access_key: ${SOJ_TEST_ACCESS:-minioadmin}
  secret_key: ${SOJ_TEST_SECRET:-minioadmin}
judge:
  sandbox_backend: docker
`)

	var out strings.Builder
	if err := Print(&out, file); err != nil {
		t.Fatalf("Print() error = %v", err)
	}
	printed := out.String()

	for _, secret := range []string{"postgres://user:password@db:5432/soj", "super-secret", "minioadmin"} {
		if strings.Contains(printed, secret) {
			t.Fatalf("Print() leaked %q:\n%s", secret, printed)
		}
	}
	if !strings.Contains(printed, masked) {
		t.Fatalf("Print() did not mask anything:\n%s", printed)
	}
	if !strings.Contains(printed, "30s") {
		t.Fatalf("Print() lost the duration format:\n%s", printed)
	}
}

func TestLoadIgnoresPlaceholdersInComments(t *testing.T) {
	cfg := loadAgent(t, `
# an example placeholder ${SOJ_TEST_UNSET} must not be resolved
http:
  addr: ":8080" # trailing ${SOJ_TEST_ALSO_UNSET}
`)
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("HTTP.Addr = %q", cfg.HTTP.Addr)
	}
}

func TestLoadExpandsPlaceholdersInsideQuotedValues(t *testing.T) {
	t.Setenv("SOJ_TEST_HASH", "with#hash")

	cfg := loadAgent(t, "storage:\n  bucket: \"${SOJ_TEST_HASH}\" # comment\n")
	if cfg.Storage.Bucket != "with#hash" {
		t.Fatalf("Storage.Bucket = %q", cfg.Storage.Bucket)
	}
}
