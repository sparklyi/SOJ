package sandbox

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"SOJ/internal/judge"
	"SOJ/internal/language"
)

// interpretedProfile has no compile step: the run command executes the source
// directly. Nothing in the sandbox may require a build.
func interpretedProfile() language.Profile {
	return language.Profile{
		Slug:       "python3",
		Name:       "Python 3",
		Version:    "3.12",
		SourceFile: "main.py",
		Compile:    nil,
		Run:        []string{"python3", "{{source}}"},
		Image:      "soj-runner-python3:test",
	}
}

func TestProcessSandboxSkipsCompilationWithoutACompileCommand(t *testing.T) {
	runner := NewProcessSandbox()
	profile := interpretedProfile()

	workspace, err := runner.Prepare(context.Background(), PrepareRequest{
		Profile: profile,
		Source:  []byte("print('hi')\n"),
		Limits:  Limits{TimeLimit: 0, MemoryKB: 262144},
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	defer func() {
		if err := runner.Cleanup(context.Background(), workspace); err != nil {
			t.Fatalf("Cleanup returned error: %v", err)
		}
	}()

	compiled, err := runner.Compile(context.Background(), workspace, profile)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if compiled.Verdict != judge.VerdictAccepted {
		t.Fatalf("verdict = %q, want accepted: an interpreted language has nothing to build", compiled.Verdict)
	}
}

func TestDockerSandboxSkipsCompilationWithoutACompileCommand(t *testing.T) {
	client := &recordingDockerClient{runOutput: commandOutput{}}
	runner := NewDockerSandbox(DockerSandboxOptions{Client: client})
	profile := interpretedProfile()

	workspace, err := runner.Prepare(context.Background(), PrepareRequest{
		Profile: profile,
		Source:  []byte("print('hi')\n"),
		Limits:  Limits{TimeLimit: 0, MemoryKB: 262144},
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	defer cleanupDockerWorkspace(t, runner, workspace)

	compiled, err := runner.Compile(context.Background(), workspace, profile)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if compiled.Verdict != judge.VerdictAccepted {
		t.Fatalf("verdict = %q, want accepted", compiled.Verdict)
	}
	if len(client.runs) != 0 {
		t.Fatalf("container runs = %d, want 0: no compile command means no container", len(client.runs))
	}
}

func TestDockerSandboxPrepareImagesPullsEachImageOnce(t *testing.T) {
	client := &recordingDockerClient{}
	runner := NewDockerSandbox(DockerSandboxOptions{Client: client})

	if err := runner.PrepareImages(context.Background(), []string{"soj-runner-rust:test", "soj-runner-go:test", "soj-runner-rust:test", "  "}); err != nil {
		t.Fatalf("PrepareImages returned error: %v", err)
	}
	want := []string{"soj-runner-go:test", "soj-runner-rust:test"}
	if !slices.Equal(client.pulls, want) {
		t.Fatalf("pulls = %v, want %v", client.pulls, want)
	}
}

func TestDockerSandboxPrepareImagesReportsTheFailingImage(t *testing.T) {
	client := &recordingDockerClient{pullErr: errors.New("registry unreachable")}
	runner := NewDockerSandbox(DockerSandboxOptions{Client: client})

	err := runner.PrepareImages(context.Background(), []string{"soj-runner-java:test"})
	if err == nil {
		t.Fatal("PrepareImages returned nil error")
	}
	if !strings.Contains(err.Error(), "soj-runner-java:test") || !strings.Contains(err.Error(), "registry unreachable") {
		t.Fatalf("error = %v, want the image and the cause", err)
	}
}
