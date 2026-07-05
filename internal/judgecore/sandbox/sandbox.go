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
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/judgecore/language"
)

type Limits struct {
	TimeLimit time.Duration
	MemoryKB  int64
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
	return Workspace{Dir: dir, SourcePath: sourcePath, BinaryPath: filepath.Join(dir, request.Profile.BinaryFilename)}, nil
}

func (s *ProcessSandbox) Compile(ctx context.Context, workspace Workspace, profile language.Profile) (CompileResult, error) {
	output, err := runCommand(ctx, workspace.Dir, render(profile.CompileCommand, workspace), "", 30*time.Second)
	if err == nil {
		return CompileResult{Verdict: judge.VerdictAccepted, Output: output.stdout + output.stderr}, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CompileResult{Verdict: judge.VerdictSystemError, Output: output.stdout + output.stderr, ErrorMessage: "compile timeout"}, nil
	}
	return CompileResult{Verdict: judge.VerdictCompileError, Output: output.stdout + output.stderr, ErrorMessage: strings.TrimSpace(output.stderr)}, nil
}

func (s *ProcessSandbox) Run(ctx context.Context, workspace Workspace, profile language.Profile, request RunRequest) (RunResult, error) {
	started := time.Now()
	output, err := runCommand(ctx, workspace.Dir, render(profile.RunCommand, workspace), request.Stdin, request.Limits.TimeLimit)
	elapsed := int(time.Since(started).Milliseconds())
	result := RunResult{Verdict: judge.VerdictAccepted, Stdout: output.stdout, Stderr: output.stderr, TimeMS: elapsed}
	if err == nil {
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
	return os.RemoveAll(workspace.Dir)
}

type commandOutput struct {
	stdout   string
	stderr   string
	exitCode *int32
}

func runCommand(parent context.Context, dir string, args []string, stdin string, timeout time.Duration) (commandOutput, error) {
	if len(args) == 0 {
		return commandOutput{}, fmt.Errorf("empty command")
	}
	ctx := parent
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	}
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := commandOutput{stdout: stdout.String(), stderr: stderr.String()}
	if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
		code := int32(exitErr.ExitCode())
		output.exitCode = &code
	}
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	return output, err
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
