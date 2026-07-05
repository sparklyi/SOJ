package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/judgecore/language"
)

const (
	defaultDockerRunnerUser = "1000:1000"
	defaultDockerPidsLimit  = 128
	defaultDockerTmpfs      = "/tmp:rw,nosuid,nodev,noexec,size=128m"
)

type DockerClient interface {
	Run(ctx context.Context, spec DockerRunSpec) (commandOutput, error)
	RemoveContainer(ctx context.Context, name string) error
	RuntimeAvailable(ctx context.Context, runtime string) (bool, error)
}

type DockerSandboxOptions struct {
	Client   DockerClient
	Runtime  string
	Images   map[string]string
	TempDir  string
	User     string
	Observer SandboxObserver
}

type DockerRunSpec struct {
	Name             string
	Image            string
	Runtime          string
	Workdir          string
	User             string
	NetworkDisabled  bool
	ReadOnlyRootFS   bool
	CapDropAll       bool
	SecurityOpt      []string
	Mounts           []DockerMount
	Tmpfs            []string
	Env              []string
	Command          []string
	Stdin            string
	TimeLimit        time.Duration
	MemoryBytes      int64
	PidsLimit        int
	OutputLimitBytes int64
	Labels           map[string]string
}

type DockerMount struct {
	HostPath      string
	ContainerPath string
	Mode          string
}

type DockerSandbox struct {
	client   DockerClient
	runtime  string
	images   map[string]string
	tempDir  string
	user     string
	observer SandboxObserver
}

type SandboxObserver interface {
	ObserveSandboxPhase(backend, phase, result string, duration time.Duration)
	RecordSandboxBackendError(backend, phase, class string)
	RecordSandboxCleanupFailure(backend string)
}

func NewDockerSandbox(options DockerSandboxOptions) *DockerSandbox {
	client := options.Client
	if client == nil {
		client = dockerCLIClient{binary: "docker"}
	}
	images := map[string]string{
		"go":    "ghcr.io/sparklyi/soj-runner-go:main",
		"cpp17": "ghcr.io/sparklyi/soj-runner-cpp17:main",
	}
	for language, image := range options.Images {
		if strings.TrimSpace(image) != "" {
			images[normalizeLanguageSlug(language)] = image
		}
	}
	user := strings.TrimSpace(options.User)
	if user == "" {
		user = defaultDockerRunnerUser
	}
	return &DockerSandbox{client: client, runtime: strings.TrimSpace(options.Runtime), images: images, tempDir: options.TempDir, user: user, observer: options.Observer}
}

func (s *DockerSandbox) Name() string {
	return BackendDocker
}

func (s *DockerSandbox) Profile() string {
	if s.runtime != "" {
		return s.runtime
	}
	return "default"
}

func (s *DockerSandbox) Probe(ctx context.Context) (Capabilities, error) {
	capabilities := Capabilities{
		Backend:         s.Name(),
		Profile:         s.Profile(),
		Runtime:         s.Profile(),
		ProductionReady: false,
	}
	if s.runtime == "" {
		capabilities.UnsafeReason = "docker runtime is not set; production requires runsc"
		return capabilities, nil
	}
	available, err := s.client.RuntimeAvailable(ctx, s.runtime)
	if err != nil {
		return Capabilities{}, err
	}
	if !available {
		capabilities.UnsafeReason = fmt.Sprintf("docker runtime %q is unavailable", s.runtime)
		return capabilities, nil
	}
	if s.runtime != "runsc" {
		capabilities.UnsafeReason = fmt.Sprintf("docker runtime %q is not the required runsc runtime", s.runtime)
		return capabilities, nil
	}
	if err := s.probeNoopContainer(ctx); err != nil {
		capabilities.UnsafeReason = fmt.Sprintf("docker runsc probe container failed: %v", err)
		return capabilities, nil
	}
	capabilities.ProductionReady = true
	return capabilities, nil
}

func (s *DockerSandbox) Prepare(ctx context.Context, request PrepareRequest) (Workspace, error) {
	dir, err := os.MkdirTemp(s.tempDir, "soj-docker-*")
	if err != nil {
		return Workspace{}, err
	}
	if err := os.Chmod(dir, 0777); err != nil {
		_ = os.RemoveAll(dir)
		return Workspace{}, err
	}
	sourcePath := filepath.Join(dir, request.Profile.SourceFilename)
	if err := os.WriteFile(sourcePath, request.Source, 0644); err != nil {
		_ = os.RemoveAll(dir)
		return Workspace{}, err
	}
	return Workspace{Dir: dir, SourcePath: sourcePath, BinaryPath: filepath.Join(dir, request.Profile.BinaryFilename), Limits: request.Limits}, nil
}

func (s *DockerSandbox) Compile(ctx context.Context, workspace Workspace, profile language.Profile) (CompileResult, error) {
	compileLimits := Limits{TimeLimit: 30 * time.Second, MemoryKB: workspace.Limits.MemoryKB, OutputLimitBytes: workspace.Limits.OutputLimitBytes}
	output, err := s.runContainer(ctx, workspace, profile, "compile", "rw", "", compileLimits, render(profile.CompileCommand, dockerWorkspace(workspace)))
	combined := output.stdout + output.stderr
	if err == nil {
		return CompileResult{Verdict: judge.VerdictAccepted, Output: combined}, nil
	}
	if errors.Is(err, errOutputLimit) {
		return CompileResult{Verdict: judge.VerdictCompileError, Output: combined, ErrorMessage: "compile output limit exceeded"}, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CompileResult{Verdict: judge.VerdictSystemError, Output: combined, ErrorMessage: "compile timeout"}, nil
	}
	return CompileResult{Verdict: judge.VerdictCompileError, Output: combined, ErrorMessage: strings.TrimSpace(output.stderr)}, nil
}

func (s *DockerSandbox) Run(ctx context.Context, workspace Workspace, profile language.Profile, request RunRequest) (RunResult, error) {
	started := time.Now()
	output, err := s.runContainer(ctx, workspace, profile, "run", "ro", request.Stdin, request.Limits, render(profile.RunCommand, dockerWorkspace(workspace)))
	elapsed := int(time.Since(started).Milliseconds())
	result := RunResult{Verdict: judge.VerdictAccepted, Stdout: output.stdout, Stderr: output.stderr, TimeMS: elapsed, ExitCode: output.exitCode, Signal: output.signal}
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
	if output.exitCode != nil && *output.exitCode == 137 {
		result.Verdict = judge.VerdictMemoryLimit
		result.ErrorMessage = "memory limit exceeded"
		return result, nil
	}
	result.Verdict = judge.VerdictRuntimeError
	result.ErrorMessage = strings.TrimSpace(output.stderr)
	return result, nil
}

func (s *DockerSandbox) Cleanup(ctx context.Context, workspace Workspace) error {
	if err := os.RemoveAll(workspace.Dir); err != nil {
		if s.observer != nil {
			s.observer.RecordSandboxCleanupFailure(BackendDocker)
		}
		return err
	}
	return nil
}

func (s *DockerSandbox) runContainer(ctx context.Context, workspace Workspace, profile language.Profile, phase, mountMode, stdin string, limits Limits, command []string) (commandOutput, error) {
	spec, err := s.runSpec(workspace, profile, phase, mountMode, stdin, limits, command)
	if err != nil {
		if s.observer != nil {
			s.observer.RecordSandboxBackendError(BackendDocker, phase, "spec_error")
		}
		return commandOutput{}, err
	}
	defer func() {
		if err := s.client.RemoveContainer(context.Background(), spec.Name); err != nil && s.observer != nil {
			s.observer.RecordSandboxCleanupFailure(BackendDocker)
		}
	}()
	started := time.Now()
	output, err := s.client.Run(ctx, spec)
	result := "success"
	if err != nil {
		result = "error"
	}
	if s.observer != nil {
		s.observer.ObserveSandboxPhase(BackendDocker, phase, result, time.Since(started))
		if err != nil && output.exitCode == nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, errOutputLimit) {
			s.observer.RecordSandboxBackendError(BackendDocker, phase, "docker_error")
		}
	}
	return output, err
}

func (s *DockerSandbox) runSpec(workspace Workspace, profile language.Profile, phase, mountMode, stdin string, limits Limits, command []string) (DockerRunSpec, error) {
	image := s.images[normalizeLanguageSlug(profile.Slug)]
	if image == "" {
		return DockerRunSpec{}, fmt.Errorf("docker runner image for language %q is not configured", profile.Slug)
	}
	if len(command) == 0 {
		return DockerRunSpec{}, fmt.Errorf("docker runner command for phase %s is empty", phase)
	}
	return DockerRunSpec{
		Name:             dockerContainerName(phase, workspace.Dir),
		Image:            image,
		Runtime:          s.runtime,
		Workdir:          "/workspace",
		User:             s.user,
		NetworkDisabled:  true,
		ReadOnlyRootFS:   true,
		CapDropAll:       true,
		SecurityOpt:      []string{"no-new-privileges"},
		Mounts:           []DockerMount{{HostPath: workspace.Dir, ContainerPath: "/workspace", Mode: mountMode}},
		Tmpfs:            []string{defaultDockerTmpfs},
		Env:              []string{"HOME=/tmp", "TMPDIR=/tmp", "GOCACHE=/tmp/go-cache", "GOMODCACHE=/tmp/go-mod"},
		Command:          command,
		Stdin:            stdin,
		TimeLimit:        limits.TimeLimit,
		MemoryBytes:      limits.MemoryKB * 1024,
		PidsLimit:        defaultDockerPidsLimit,
		OutputLimitBytes: outputLimit(limits.OutputLimitBytes),
		Labels: map[string]string{
			"soj.sandbox":   BackendDocker,
			"soj.phase":     phase,
			"soj.language":  profile.Slug,
			"soj.workspace": filepath.Base(workspace.Dir),
		},
	}, nil
}

func (s *DockerSandbox) probeNoopContainer(ctx context.Context) error {
	image := s.images["go"]
	if image == "" {
		return fmt.Errorf("go runner image is not configured")
	}
	spec := DockerRunSpec{
		Name:             dockerContainerName("probe", "probe"),
		Image:            image,
		Runtime:          s.runtime,
		Workdir:          "/workspace",
		User:             s.user,
		NetworkDisabled:  true,
		ReadOnlyRootFS:   true,
		CapDropAll:       true,
		SecurityOpt:      []string{"no-new-privileges"},
		Tmpfs:            []string{defaultDockerTmpfs},
		Env:              []string{"HOME=/tmp", "TMPDIR=/tmp"},
		Command:          []string{"sh", "-lc", `test "$(id -u)" = "1000" && test ! -e /var/run/docker.sock`},
		TimeLimit:        5 * time.Second,
		MemoryBytes:      128 * 1024 * 1024,
		PidsLimit:        32,
		OutputLimitBytes: 64 * 1024,
		Labels: map[string]string{
			"soj.sandbox": BackendDocker,
			"soj.phase":   "probe",
		},
	}
	defer func() {
		if err := s.client.RemoveContainer(context.Background(), spec.Name); err != nil && s.observer != nil {
			s.observer.RecordSandboxCleanupFailure(BackendDocker)
		}
	}()
	_, err := s.client.Run(ctx, spec)
	return err
}

func dockerWorkspace(workspace Workspace) Workspace {
	out := workspace
	out.SourcePath = filepath.ToSlash(filepath.Join("/workspace", filepath.Base(workspace.SourcePath)))
	out.BinaryPath = filepath.ToSlash(filepath.Join("/workspace", filepath.Base(workspace.BinaryPath)))
	return out
}

func dockerContainerName(phase, workspaceDir string) string {
	base := strings.NewReplacer(".", "-", "_", "-", string(os.PathSeparator), "-").Replace(filepath.Base(workspaceDir))
	return fmt.Sprintf("soj-%s-%s-%d", phase, base, time.Now().UnixNano())
}

func normalizeLanguageSlug(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

type dockerCLIClient struct {
	binary string
}

func (c dockerCLIClient) Run(ctx context.Context, spec DockerRunSpec) (commandOutput, error) {
	return runDockerCLI(ctx, c.binary, dockerRunArgs(spec), spec.Stdin, spec.TimeLimit, spec.OutputLimitBytes)
}

func (c dockerCLIClient) RemoveContainer(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, c.binary, "rm", "-f", name)
	output, err := cmd.CombinedOutput()
	if err == nil || strings.Contains(string(output), "No such container") {
		return nil
	}
	return err
}

func (c dockerCLIClient) RuntimeAvailable(ctx context.Context, runtime string) (bool, error) {
	if strings.TrimSpace(runtime) == "" {
		return true, nil
	}
	cmd := exec.CommandContext(ctx, c.binary, "info", "--format", "{{json .Runtimes}}")
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.Contains(string(output), strconv.Quote(runtime)), nil
}

func dockerRunArgs(spec DockerRunSpec) []string {
	args := []string{"run", "--rm", "--name", spec.Name}
	if spec.Stdin != "" {
		args = append(args, "--interactive")
	}
	if spec.Runtime != "" {
		args = append(args, "--runtime", spec.Runtime)
	}
	if spec.NetworkDisabled {
		args = append(args, "--network", "none")
	}
	if spec.ReadOnlyRootFS {
		args = append(args, "--read-only")
	}
	if spec.CapDropAll {
		args = append(args, "--cap-drop", "ALL")
	}
	for _, item := range spec.SecurityOpt {
		args = append(args, "--security-opt", item)
	}
	if spec.User != "" {
		args = append(args, "--user", spec.User)
	}
	if spec.Workdir != "" {
		args = append(args, "--workdir", spec.Workdir)
	}
	if spec.MemoryBytes > 0 {
		args = append(args, "--memory", strconv.FormatInt(spec.MemoryBytes, 10))
	}
	if spec.PidsLimit > 0 {
		args = append(args, "--pids-limit", strconv.Itoa(spec.PidsLimit))
	}
	for _, tmpfs := range spec.Tmpfs {
		args = append(args, "--tmpfs", tmpfs)
	}
	for _, env := range spec.Env {
		args = append(args, "-e", env)
	}
	for _, mount := range spec.Mounts {
		args = append(args, "-v", fmt.Sprintf("%s:%s:%s", mount.HostPath, mount.ContainerPath, mount.Mode))
	}
	labels := make([]string, 0, len(spec.Labels))
	for key := range spec.Labels {
		labels = append(labels, key)
	}
	sort.Strings(labels)
	for _, key := range labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, spec.Labels[key]))
	}
	args = append(args, spec.Image)
	args = append(args, spec.Command...)
	return args
}

func runDockerCLI(parent context.Context, binary string, args []string, stdin string, timeout time.Duration, outputLimitBytes int64) (commandOutput, error) {
	ctx := parent
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	}
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = strings.NewReader(stdin)
	limiter := &outputLimiter{limit: outputLimit(outputLimitBytes)}
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
	if outputSizeExceeded(output, outputLimitBytes) {
		return output, errOutputLimit
	}
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	return output, err
}
