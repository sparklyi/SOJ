package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/judgecore/language"
)

const DefaultCleanupTimeout = 5 * time.Second

type Limits struct {
	TimeLimit        time.Duration
	MemoryKB         int64
	OutputLimitBytes int64
}

type PrepareRequest struct {
	Profile language.Profile
	Source  []byte
	Limits  Limits
}

type Workspace struct {
	Dir        string
	SourcePath string
	BinaryPath string
	Limits     Limits
}

type CompileResult struct {
	Verdict      judge.Verdict
	Output       string
	ErrorMessage string
}

type RunRequest struct {
	Stdin  string
	Limits Limits
}

type RunResult struct {
	Verdict      judge.Verdict
	Stdout       string
	Stderr       string
	TimeMS       int
	MemoryKB     int
	ExitCode     *int32
	Signal       string
	ErrorMessage string
}

type Sandbox interface {
	Name() string
	Profile() string
	Probe(ctx context.Context) (Capabilities, error)
	Prepare(ctx context.Context, request PrepareRequest) (Workspace, error)
	Compile(ctx context.Context, workspace Workspace, profile language.Profile) (CompileResult, error)
	Run(ctx context.Context, workspace Workspace, profile language.Profile, request RunRequest) (RunResult, error)
	Cleanup(ctx context.Context, workspace Workspace) error
}

type ProcessSandbox struct {
	tempDir string
}

func NewProcessSandbox() *ProcessSandbox {
	return &ProcessSandbox{}
}

func (s *ProcessSandbox) Name() string {
	return "process"
}

func (s *ProcessSandbox) Profile() string {
	return "dev"
}

func (s *ProcessSandbox) Probe(ctx context.Context) (Capabilities, error) {
	return Capabilities{
		Backend:         s.Name(),
		Profile:         s.Profile(),
		Runtime:         "host-process",
		ProductionReady: false,
		UnsafeReason:    "process backend is not isolated",
	}, nil
}

func (s *ProcessSandbox) Prepare(ctx context.Context, request PrepareRequest) (Workspace, error) {
	dir, err := os.MkdirTemp(s.tempDir, "soj-judge-*")
	if err != nil {
		return Workspace{}, err
	}
	sourcePath := filepath.Join(dir, request.Profile.SourceFilename)
	if err := os.WriteFile(sourcePath, request.Source, 0600); err != nil {
		_ = os.RemoveAll(dir)
		return Workspace{}, err
	}
	return Workspace{Dir: dir, SourcePath: sourcePath, BinaryPath: filepath.Join(dir, request.Profile.BinaryFilename), Limits: request.Limits}, nil
}

func (s *ProcessSandbox) Compile(ctx context.Context, workspace Workspace, profile language.Profile) (CompileResult, error) {
	compileLimits := Limits{TimeLimit: 30 * time.Second, OutputLimitBytes: workspace.Limits.OutputLimitBytes}
	output, err := runCommand(ctx, workspace.Dir, render(profile.CompileCommand, workspace), "", compileLimits)
	if err == nil {
		return CompileResult{Verdict: judge.VerdictAccepted, Output: output.stdout + output.stderr}, nil
	}
	if errors.Is(err, errOutputLimit) {
		return CompileResult{Verdict: judge.VerdictCompileError, Output: output.stdout + output.stderr, ErrorMessage: "compile output limit exceeded"}, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CompileResult{Verdict: judge.VerdictSystemError, Output: output.stdout + output.stderr, ErrorMessage: "compile timeout"}, nil
	}
	return CompileResult{Verdict: judge.VerdictCompileError, Output: output.stdout + output.stderr, ErrorMessage: strings.TrimSpace(output.stderr)}, nil
}

func (s *ProcessSandbox) Run(ctx context.Context, workspace Workspace, profile language.Profile, request RunRequest) (RunResult, error) {
	started := time.Now()
	output, err := runCommand(ctx, workspace.Dir, render(profile.RunCommand, workspace), request.Stdin, request.Limits)
	elapsed := int(time.Since(started).Milliseconds())
	result := RunResult{Verdict: judge.VerdictAccepted, Stdout: output.stdout, Stderr: output.stderr, TimeMS: elapsed, Signal: output.signal}
	if err == nil {
		return result, nil
	}
	if errors.Is(err, errOutputLimit) {
		result.Verdict = judge.VerdictOutputLimit
		result.ErrorMessage = "output limit exceeded"
		return result, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		result.Verdict = judge.VerdictTimeLimit
		result.ErrorMessage = "time limit exceeded"
		return result, nil
	}
	result.Verdict = judge.VerdictRuntimeError
	result.ErrorMessage = strings.TrimSpace(output.stderr)
	result.ExitCode = output.exitCode
	return result, nil
}

func (s *ProcessSandbox) Cleanup(ctx context.Context, workspace Workspace) error {
	return removeWorkspace(ctx, workspace.Dir)
}

func removeWorkspace(ctx context.Context, dir string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// os.RemoveAll does not accept a context, so keep the caller's cleanup deadline enforceable.
	result := make(chan error, 1)
	go func() {
		result <- os.RemoveAll(dir)
	}()

	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type commandOutput struct {
	stdout   string
	stderr   string
	exitCode *int32
	signal   string
}

var errOutputLimit = errors.New("output limit exceeded")

const defaultOutputLimitBytes int64 = 1 << 20

func runCommand(parent context.Context, dir string, args []string, stdin string, limits Limits) (commandOutput, error) {
	if len(args) == 0 {
		return commandOutput{}, fmt.Errorf("empty command")
	}
	ctx := parent
	cancel := func() {}
	if limits.TimeLimit > 0 {
		ctx, cancel = context.WithTimeout(parent, limits.TimeLimit)
	}
	defer cancel()
	cmdArgs := resourceWrappedArgs(args, limits)
	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	limiter := &outputLimiter{limit: outputLimit(limits.OutputLimitBytes)}
	var stdout, stderr limitedBuffer
	stdout.limiter = limiter
	stderr.limiter = limiter
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := commandOutput{stdout: stdout.String(), stderr: stderr.String()}
	if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
		code := int32(exitErr.ExitCode())
		output.exitCode = &code
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			output.signal = status.Signal().String()
		}
	}
	if limiter.exceeded {
		return output, errOutputLimit
	}
	if outputSizeExceeded(output, limits.OutputLimitBytes) {
		return output, errOutputLimit
	}
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	return output, err
}

type outputLimiter struct {
	limit    int64
	written  int64
	exceeded bool
}

type limitedBuffer struct {
	bytes.Buffer
	limiter *outputLimiter
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limiter == nil || b.limiter.limit <= 0 {
		return b.Buffer.Write(p)
	}
	remaining := b.limiter.limit - b.limiter.written
	if remaining <= 0 {
		b.limiter.exceeded = true
		return 0, errOutputLimit
	}
	if int64(len(p)) > remaining {
		n, _ := b.Buffer.Write(p[:remaining])
		b.limiter.written += int64(n)
		b.limiter.exceeded = true
		return n, errOutputLimit
	}
	n, err := b.Buffer.Write(p)
	b.limiter.written += int64(n)
	return n, err
}

func outputLimit(limit int64) int64 {
	if limit > 0 {
		return limit
	}
	return defaultOutputLimitBytes
}

func outputSizeExceeded(output commandOutput, limit int64) bool {
	if limit <= 0 {
		return false
	}
	return int64(len(output.stdout)+len(output.stderr)) >= limit
}

func resourceWrappedArgs(args []string, limits Limits) []string {
	commands := make([]string, 0, 4)
	commands = append(commands, `exec "$@"`)
	wrapped := []string{"sh", "-c", strings.Join(commands, "; "), "soj-command"}
	return append(wrapped, args...)
}

func render(args []string, workspace Workspace) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.ReplaceAll(arg, "{{source}}", workspace.SourcePath)
		arg = strings.ReplaceAll(arg, "{{binary}}", workspace.BinaryPath)
		out = append(out, arg)
	}
	return out
}
