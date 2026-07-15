package sandbox

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/judgecore/language"
)

func TestDockerSandboxCompileUsesSecureContainerSpec(t *testing.T) {
	client := &recordingDockerClient{runOutput: commandOutput{}}
	s := NewDockerSandbox(DockerSandboxOptions{
		Client:  client,
		Runtime: "runsc",
		Images:  map[string]string{"go": "soj-runner-go:test"},
	})
	workspace, err := s.Prepare(context.Background(), PrepareRequest{
		Profile: language.GoProfile(),
		Source:  []byte("package main\nfunc main() {}\n"),
		Limits:  Limits{TimeLimit: time.Second, MemoryKB: 262144, OutputLimitBytes: 1024},
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	cleanupDockerWorkspace(t, s, workspace)

	compiled, err := s.Compile(context.Background(), workspace, language.GoProfile())
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if compiled.Verdict != judge.VerdictAccepted {
		t.Fatalf("compile verdict = %q", compiled.Verdict)
	}
	if len(client.runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(client.runs))
	}
	spec := client.runs[0]
	if spec.Runtime != "runsc" || spec.Image != "soj-runner-go:test" {
		t.Fatalf("runtime/image = %q/%q", spec.Runtime, spec.Image)
	}
	if !spec.NetworkDisabled || !spec.ReadOnlyRootFS || !spec.CapDropAll || !contains(spec.SecurityOpt, "no-new-privileges") {
		t.Fatalf("insecure container spec = %+v", spec)
	}
	if spec.User == "" || spec.PidsLimit == 0 || spec.MemoryBytes == 0 || spec.OutputLimitBytes != 1024 {
		t.Fatalf("resource spec = %+v", spec)
	}
	if len(spec.Mounts) != 1 || spec.Mounts[0].ContainerPath != "/workspace" || spec.Mounts[0].Mode != "rw" {
		t.Fatalf("mounts = %+v", spec.Mounts)
	}
	if containsPrefix(spec.Env, "SOJ_DATABASE_DSN=") || containsPrefix(spec.Env, "SOJ_REDIS_ADDR=") || containsPrefix(spec.Env, "SOJ_STORAGE_SECRET_KEY=") {
		t.Fatalf("runner env leaked business credentials: %+v", spec.Env)
	}
}

func TestDockerSandboxRunMapsVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		output  commandOutput
		err     error
		want    judge.Verdict
		message string
	}{
		{name: "accepted", output: commandOutput{stdout: "3\n"}, want: judge.VerdictAccepted},
		{name: "runtime error", output: commandOutput{stderr: "panic", exitCode: int32Ptr(2)}, err: errors.New("exit status 2"), want: judge.VerdictRuntimeError, message: "panic"},
		{name: "time limit", err: context.DeadlineExceeded, want: judge.VerdictTimeLimit, message: "time limit exceeded"},
		{name: "output limit", err: errOutputLimit, want: judge.VerdictOutputLimit, message: "output limit exceeded"},
		{name: "memory limit", output: commandOutput{stderr: "killed", exitCode: int32Ptr(137)}, err: errors.New("exit status 137"), want: judge.VerdictMemoryLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &recordingDockerClient{runOutput: tc.output, runErr: tc.err}
			s := NewDockerSandbox(DockerSandboxOptions{Client: client, Images: map[string]string{"go": "soj-runner-go:test"}})
			workspace, err := s.Prepare(context.Background(), PrepareRequest{Profile: language.GoProfile(), Source: []byte("package main\nfunc main() {}\n")})
			if err != nil {
				t.Fatalf("Prepare returned error: %v", err)
			}
			cleanupDockerWorkspace(t, s, workspace)
			result, err := s.Run(context.Background(), workspace, language.GoProfile(), RunRequest{Stdin: "1 2\n", Limits: Limits{TimeLimit: time.Second}})
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if result.Verdict != tc.want {
				t.Fatalf("verdict = %q, want %q", result.Verdict, tc.want)
			}
			if tc.message != "" && !strings.Contains(result.ErrorMessage, tc.message) {
				t.Fatalf("error message = %q, want %q", result.ErrorMessage, tc.message)
			}
		})
	}
}

func TestDockerSandboxObserverRecordsPhaseDuration(t *testing.T) {
	client := &recordingDockerClient{runOutput: commandOutput{stdout: "3\n"}}
	observer := &recordingSandboxObserver{}
	s := NewDockerSandbox(DockerSandboxOptions{Client: client, Observer: observer, Images: map[string]string{"go": "soj-runner-go:test"}})
	workspace, err := s.Prepare(context.Background(), PrepareRequest{Profile: language.GoProfile(), Source: []byte("package main\nfunc main() {}\n")})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	cleanupDockerWorkspace(t, s, workspace)

	if _, err := s.Run(context.Background(), workspace, language.GoProfile(), RunRequest{Stdin: "1 2\n", Limits: Limits{TimeLimit: time.Second}}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(observer.phases) != 1 || observer.phases[0].backend != BackendDocker || observer.phases[0].phase != "run" || observer.phases[0].result != "success" {
		t.Fatalf("observer phases = %+v", observer.phases)
	}
}

func TestDockerSandboxObserverRecordsContainerCleanupFailure(t *testing.T) {
	client := &recordingDockerClient{runOutput: commandOutput{stdout: "3\n"}, removeErr: errors.New("remove failed")}
	observer := &recordingSandboxObserver{}
	s := NewDockerSandbox(DockerSandboxOptions{Client: client, Observer: observer, Images: map[string]string{"go": "soj-runner-go:test"}})
	workspace, err := s.Prepare(context.Background(), PrepareRequest{Profile: language.GoProfile(), Source: []byte("package main\nfunc main() {}\n")})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	cleanupDockerWorkspace(t, s, workspace)

	if _, err := s.Run(context.Background(), workspace, language.GoProfile(), RunRequest{Limits: Limits{TimeLimit: time.Second}}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if observer.cleanupFailures != 1 {
		t.Fatalf("cleanup failures = %d, want 1", observer.cleanupFailures)
	}
}

func TestDockerSandboxContainerCleanupUsesDeadline(t *testing.T) {
	cleanupTimeout := 25 * time.Millisecond
	client := &recordingDockerClient{runOutput: commandOutput{stdout: "3\n"}, waitForRemoveDeadline: true}
	observer := &recordingSandboxObserver{}
	s := NewDockerSandbox(DockerSandboxOptions{
		Client:         client,
		CleanupTimeout: cleanupTimeout,
		Observer:       observer,
		Images:         map[string]string{"go": "soj-runner-go:test"},
	})
	workspace, err := s.Prepare(context.Background(), PrepareRequest{Profile: language.GoProfile(), Source: []byte("package main\nfunc main() {}\n")})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	cleanupDockerWorkspace(t, s, workspace)

	started := time.Now()
	if _, err := s.Run(context.Background(), workspace, language.GoProfile(), RunRequest{Limits: Limits{TimeLimit: time.Second}}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	elapsed := time.Since(started)

	if !client.removeSawDeadline {
		t.Fatal("RemoveContainer context did not have a deadline")
	}
	if elapsed < cleanupTimeout/2 {
		t.Fatalf("Run returned after %v, want cleanup to wait for its %v deadline", elapsed, cleanupTimeout)
	}
	if elapsed > time.Second {
		t.Fatalf("Run returned after %v, cleanup deadline was not enforced", elapsed)
	}
	if observer.cleanupFailures != 1 || observer.cleanupTimeouts != 1 {
		t.Fatalf("cleanup metrics = failures:%d timeouts:%d, want 1 each", observer.cleanupFailures, observer.cleanupTimeouts)
	}
	if len(observer.cleanupTimeoutResources) != 1 || observer.cleanupTimeoutResources[0] != cleanupResourceContainer {
		t.Fatalf("cleanup timeout resources = %v, want %q", observer.cleanupTimeoutResources, cleanupResourceContainer)
	}
}

func TestDockerSandboxWorkspaceCleanupRecordsTimeout(t *testing.T) {
	observer := &recordingSandboxObserver{}
	var logs bytes.Buffer
	s := NewDockerSandbox(DockerSandboxOptions{
		Observer: observer,
		Logger:   slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	err := s.Cleanup(ctx, Workspace{Dir: t.TempDir()})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Cleanup error = %v, want context deadline exceeded", err)
	}
	if observer.cleanupFailures != 1 || observer.cleanupTimeouts != 1 {
		t.Fatalf("cleanup metrics = failures:%d timeouts:%d, want 1 each", observer.cleanupFailures, observer.cleanupTimeouts)
	}
	if len(observer.cleanupTimeoutResources) != 1 || observer.cleanupTimeoutResources[0] != cleanupResourceWorkspace {
		t.Fatalf("cleanup timeout resources = %v, want %q", observer.cleanupTimeoutResources, cleanupResourceWorkspace)
	}
	if !strings.Contains(logs.String(), `"resource":"workspace"`) || !strings.Contains(logs.String(), `"timeout":true`) {
		t.Fatalf("cleanup log = %q, want workspace timeout fields", logs.String())
	}
}

func TestDockerSandboxCompileMapsCompileError(t *testing.T) {
	client := &recordingDockerClient{runOutput: commandOutput{stderr: "syntax error", exitCode: int32Ptr(1)}, runErr: errors.New("exit status 1")}
	s := NewDockerSandbox(DockerSandboxOptions{Client: client, Images: map[string]string{"cpp17": "soj-runner-cpp17:test"}})
	workspace, err := s.Prepare(context.Background(), PrepareRequest{Profile: language.Cpp17Profile(), Source: []byte("bad")})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	cleanupDockerWorkspace(t, s, workspace)

	compiled, err := s.Compile(context.Background(), workspace, language.Cpp17Profile())
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if compiled.Verdict != judge.VerdictCompileError || !strings.Contains(compiled.ErrorMessage, "syntax error") {
		t.Fatalf("compile result = %+v", compiled)
	}
}

func TestDockerSandboxProbeReportsRunscReadiness(t *testing.T) {
	client := &recordingDockerClient{runtimeAvailable: true}
	s := NewDockerSandbox(DockerSandboxOptions{Client: client, Runtime: "runsc"})
	capabilities, err := s.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if !capabilities.ProductionReady || capabilities.Runtime != "runsc" {
		t.Fatalf("capabilities = %+v", capabilities)
	}
	if len(client.runs) != 1 {
		t.Fatalf("probe runs = %d, want 1", len(client.runs))
	}
	probe := client.runs[0]
	if probe.Runtime != "runsc" || !probe.NetworkDisabled || !probe.ReadOnlyRootFS || probe.User == "" || !probe.CapDropAll {
		t.Fatalf("probe spec is not production-shaped: %+v", probe)
	}

	client.runtimeAvailable = false
	client.runs = nil
	capabilities, err = s.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if capabilities.ProductionReady || !strings.Contains(capabilities.UnsafeReason, "runsc") {
		t.Fatalf("capabilities = %+v, want unsafe runsc reason", capabilities)
	}
}

func TestDockerRunArgsKeepsStdinOpenWhenProvided(t *testing.T) {
	args := dockerRunArgs(DockerRunSpec{
		Name:    "soj-run",
		Image:   "soj-runner-go:test",
		Stdin:   "1 1\n",
		Command: []string{"/workspace/main"},
	})
	if !contains(args, "--interactive") {
		t.Fatalf("docker run args = %v, want --interactive for stdin", args)
	}
}

func cleanupDockerWorkspace(t *testing.T, s *DockerSandbox, workspace Workspace) {
	t.Helper()
	t.Cleanup(func() {
		if err := s.Cleanup(context.Background(), workspace); err != nil {
			t.Errorf("Cleanup returned error: %v", err)
		}
	})
}

type recordingDockerClient struct {
	runs                  []DockerRunSpec
	runOutput             commandOutput
	runErr                error
	removeErr             error
	removeSawDeadline     bool
	waitForRemoveDeadline bool
	runtimeAvailable      bool
}

func (c *recordingDockerClient) Run(ctx context.Context, spec DockerRunSpec) (commandOutput, error) {
	c.runs = append(c.runs, spec)
	return c.runOutput, c.runErr
}

func (c *recordingDockerClient) RemoveContainer(ctx context.Context, name string) error {
	if _, ok := ctx.Deadline(); ok {
		c.removeSawDeadline = true
	} else if c.waitForRemoveDeadline {
		return c.removeErr
	}
	if c.waitForRemoveDeadline {
		<-ctx.Done()
		return ctx.Err()
	}
	return c.removeErr
}

func (c *recordingDockerClient) RuntimeAvailable(ctx context.Context, runtime string) (bool, error) {
	return c.runtimeAvailable, nil
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func containsPrefix(items []string, prefix string) bool {
	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}

func int32Ptr(value int32) *int32 {
	return &value
}

type recordingSandboxObserver struct {
	phases                  []recordedSandboxPhase
	cleanupFailures         int
	cleanupTimeouts         int
	cleanupFailureResources []string
	cleanupTimeoutResources []string
}

type recordedSandboxPhase struct {
	backend string
	phase   string
	result  string
}

func (o *recordingSandboxObserver) ObserveSandboxPhase(backend, phase, result string, duration time.Duration) {
	o.phases = append(o.phases, recordedSandboxPhase{backend: backend, phase: phase, result: result})
}

func (o *recordingSandboxObserver) RecordSandboxBackendError(backend, phase, class string) {}

func (o *recordingSandboxObserver) RecordSandboxCleanupFailure(backend, resource string) {
	o.cleanupFailures++
	o.cleanupFailureResources = append(o.cleanupFailureResources, resource)
}

func (o *recordingSandboxObserver) RecordSandboxCleanupTimeout(backend, resource string) {
	o.cleanupTimeouts++
	o.cleanupTimeoutResources = append(o.cleanupTimeoutResources, resource)
}
