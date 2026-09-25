package sandbox

import (
	"context"
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
