package sandbox

import (
	"strings"
	"testing"
)

func TestSelectBackendRejectsProcessOutsideDevelopment(t *testing.T) {
	_, err := SelectBackend("prod", BackendProcess, "")
	if err == nil || !strings.Contains(err.Error(), "process sandbox backend is not allowed") {
		t.Fatalf("err = %v, want process rejection", err)
	}
}

func TestSelectBackendAllowsProcessInDevelopment(t *testing.T) {
	got, err := SelectBackend("dev", BackendProcess, "")
	if err != nil {
		t.Fatalf("SelectBackend returned error: %v", err)
	}
	if got != BackendProcess {
		t.Fatalf("backend = %q, want process", got)
	}
}

func TestSelectBackendDefaultsFakeForFakeJudgeEndpoint(t *testing.T) {
	got, err := SelectBackend("docker", "", "fake://accepted")
	if err != nil {
		t.Fatalf("SelectBackend returned error: %v", err)
	}
	if got != BackendFake {
		t.Fatalf("backend = %q, want fake", got)
	}
}

func TestSelectBackendRejectsUnknownBackend(t *testing.T) {
	_, err := SelectBackend("dev", "unknown", "")
	if err == nil || !strings.Contains(err.Error(), "unsupported sandbox backend") {
		t.Fatalf("err = %v, want unsupported backend", err)
	}
}
