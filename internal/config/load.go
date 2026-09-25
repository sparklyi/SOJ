package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"SOJ/internal/judgecore/sandbox"

	"go.yaml.in/yaml/v3"
)

// Role names the process a configuration is loaded for. Roles differ in what
// they require: the judge agent never talks to PostgreSQL or signs tokens.
type Role string

const (
	RoleAPI        Role = "api"
	RoleWorker     Role = "worker"
	RoleJudgeAgent Role = "judge-agent"
	RoleMigrate    Role = "migrate"
)

// Agent stream selections, used by redis.agent_streams.
const (
	AgentStreamsAll         = "all"
	AgentStreamsSubmissions = "submissions"
	AgentStreamsRuns        = "runs"
)

// defaultFile is read when no file was requested and it exists. Operators who
// keep their configuration elsewhere set SOJ_CONFIG_FILE or pass --config.
const defaultFile = "config.yaml"

// masked replaces secret values in printed configurations.
const masked = "***"

// Options selects what Load reads.
type Options struct {
	Role Role
	// File is the --config flag. Empty falls back to SOJ_CONFIG_FILE, then
	// ./config.yaml when that exists.
	File string
}

// Flags holds the configuration flags shared by every command.
type Flags struct {
	File  string
	Print bool
}

// RegisterFlags adds the shared configuration flags to a command's flag set.
func RegisterFlags(fs *flag.FlagSet) *Flags {
	flags := &Flags{}
	fs.StringVar(&flags.File, "config", "", "YAML configuration file (default: $SOJ_CONFIG_FILE, then ./config.yaml)")
	fs.BoolVar(&flags.Print, "print-config", false, "print the effective configuration with secrets masked, then exit")
	return flags
}

// Load returns the effective configuration: defaults, then the optional YAML
// file, then the ${VAR} references that file contains. It validates the result
// for the given role and reports every problem at once.
func Load(opts Options) (Config, error) {
	cfg, err := read(opts.File)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.validate(opts.Role); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Print writes the effective configuration to w with secrets masked. It skips
// validation so a broken deployment can still be inspected.
func Print(w io.Writer, file string) error {
	cfg, err := read(file)
	if err != nil {
		return err
	}
	cfg.maskSecrets()

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}

// read applies defaults, the configuration file, and the file's ${VAR}
// references, then derives the values that depend on other values.
func read(file string) (Config, error) {
	cfg := defaults()

	path, err := resolveFile(file)
	if err != nil {
		return Config{}, err
	}
	if path != "" {
		if err := loadFile(&cfg, path); err != nil {
			return Config{}, err
		}
	}

	cfg.normalize()
	return cfg, nil
}

// resolveFile picks the configuration file: the flag, then SOJ_CONFIG_FILE,
// then ./config.yaml when it exists. An explicit path is returned even if it
// does not exist, so loading reports the missing file.
func resolveFile(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if value := os.Getenv("SOJ_CONFIG_FILE"); value != "" {
		return value, nil
	}
	if _, err := os.Stat(defaultFile); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return defaultFile, nil
}

func loadFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	expanded, err := expandEnv(string(data))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	decoder := yaml.NewDecoder(strings.NewReader(expanded))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// envRefPattern matches ${NAME} and ${NAME:-default}.
var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// expandEnv replaces ${NAME} and ${NAME:-default} with environment values.
//
// A reference without a default must resolve to a non-empty value; anything
// else is collected and reported, so a missing secret stops the process
// instead of starting it with an empty one. YAML comments are left alone: a
// placeholder written in a comment documents the file, it does not configure
// it.
func expandEnv(data string) (string, error) {
	var missing []string
	var expanded strings.Builder
	expanded.Grow(len(data))

	for _, line := range strings.SplitAfter(data, "\n") {
		code, comment := splitComment(line)
		expanded.WriteString(envRefPattern.ReplaceAllStringFunc(code, func(ref string) string {
			match := envRefPattern.FindStringSubmatch(ref)
			if value, ok := os.LookupEnv(match[1]); ok && value != "" {
				return value
			}
			if match[2] != "" {
				return match[3]
			}
			missing = append(missing, match[1])
			return ref
		}))
		expanded.WriteString(comment)
	}

	if len(missing) == 0 {
		return expanded.String(), nil
	}
	slices.Sort(missing)
	return "", fmt.Errorf("undefined environment variable(s): %s", strings.Join(slices.Compact(missing), ", "))
}

// splitComment splits a line into the part the loader expands and a trailing
// comment, which starts at the first # outside quotes.
func splitComment(line string) (code, comment string) {
	var quote byte

	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return line[:i], line[i:]
		}
	}
	return line, ""
}

func (c Config) validate(role Role) error {
	var errs []error

	if strings.TrimSpace(c.Env) == "" {
		errs = append(errs, errors.New("env must not be empty"))
	}
	if c.HTTP.Addr == "" {
		errs = append(errs, errors.New("http.addr must not be empty"))
	}
	for _, check := range []struct {
		name  string
		value int64
	}{
		{"redis.batch_size", int64(c.Redis.BatchSize)},
		{"redis.stream_max_len", c.Redis.StreamMaxLen},
		{"redis.dead_stream_max_len", c.Redis.DeadStreamMaxLen},
		{"judge.parallelism", int64(c.Judge.Parallelism)},
		{"judge.max_batch", int64(c.Judge.MaxBatch)},
		{"judge.run_parallelism", int64(c.Judge.RunParallelism)},
		{"judge.run_per_user", int64(c.Judge.RunPerUser)},
		{"judge.run_stdin_max_bytes", int64(c.Judge.RunStdinMaxBytes)},
		{"retention.run_batch", int64(c.Retention.RunBatch)},
	} {
		if check.value <= 0 {
			errs = append(errs, fmt.Errorf("%s must be greater than zero", check.name))
		}
	}
	if c.Retention.RunDays < 0 {
		errs = append(errs, errors.New("retention.run_days must not be negative"))
	}
	for _, check := range []struct {
		name  string
		value int64
	}{
		{"http.read_timeout", int64(c.HTTP.ReadTimeout)},
		{"http.write_timeout", int64(c.HTTP.WriteTimeout)},
		{"worker.shutdown_timeout", int64(c.Worker.ShutdownTimeout)},
		{"redis.block", int64(c.Redis.Block)},
		{"judge.timeout", int64(c.Judge.Timeout)},
		{"judge.cleanup_timeout", int64(c.Judge.CleanupTimeout)},
		{"auth.access_token_ttl", int64(c.Auth.AccessTokenTTL)},
		{"auth.refresh_token_ttl", int64(c.Auth.RefreshTokenTTL)},
		{"retention.run_interval", int64(c.Retention.RunInterval)},
	} {
		if check.value <= 0 {
			errs = append(errs, fmt.Errorf("%s must be greater than zero", check.name))
		}
	}

	switch c.Judge.SandboxBackend {
	case "", sandbox.BackendFake, sandbox.BackendProcess, sandbox.BackendDocker, sandbox.BackendIsolate:
	default:
		errs = append(errs, fmt.Errorf("judge.sandbox_backend must be one of %q, %q, %q, %q, got %q",
			sandbox.BackendFake, sandbox.BackendProcess, sandbox.BackendDocker, sandbox.BackendIsolate, c.Judge.SandboxBackend))
	}

	switch c.Redis.AgentStreams {
	case AgentStreamsAll, AgentStreamsSubmissions, AgentStreamsRuns:
	default:
		errs = append(errs, fmt.Errorf("redis.agent_streams must be %q, %q or %q, got %q",
			AgentStreamsAll, AgentStreamsSubmissions, AgentStreamsRuns, c.Redis.AgentStreams))
	}

	if role != RoleJudgeAgent && c.Database.DSN == "" {
		errs = append(errs, fmt.Errorf("database.dsn is required for the %s command", role))
	}
	if role == RoleAPI && c.Auth.JWTSecret == "" {
		errs = append(errs, errors.New("auth.jwt_secret is required for the api command"))
	}

	return errors.Join(errs...)
}

// maskSecrets replaces every field tagged secret with a placeholder, in place.
func (c *Config) maskSecrets() {
	maskStruct(reflect.ValueOf(c).Elem())
}

func maskStruct(value reflect.Value) {
	typ := value.Type()
	for i := range value.NumField() {
		field := value.Field(i)
		if typ.Field(i).Tag.Get("secret") == "true" {
			if field.Kind() == reflect.String && field.String() != "" {
				field.SetString(masked)
			}
			continue
		}
		if field.Kind() == reflect.Struct {
			maskStruct(field)
		}
	}
}
