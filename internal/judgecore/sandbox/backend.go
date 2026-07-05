package sandbox

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	BackendProcess = "process"
	BackendIsolate = "isolate"
	BackendFake    = "fake"
)

func SelectBackend(env, configured, judgeEndpoint string) (string, error) {
	backend := strings.TrimSpace(configured)
	if backend == "" {
		if strings.HasPrefix(strings.TrimSpace(judgeEndpoint), "fake://") {
			backend = BackendFake
		} else if developmentEnv(env) {
			backend = BackendProcess
		} else {
			backend = BackendIsolate
		}
	}
	if backend == BackendProcess && !developmentEnv(env) {
		return "", fmt.Errorf("process sandbox backend is not allowed in %s environment", env)
	}
	if backend == BackendIsolate {
		if err := ProbeIsolate(); err != nil {
			return "", err
		}
	}
	switch backend {
	case BackendProcess, BackendIsolate, BackendFake:
		return backend, nil
	default:
		return "", fmt.Errorf("unsupported sandbox backend %q", backend)
	}
}

func ProbeIsolate() error {
	if _, err := exec.LookPath("isolate"); err != nil {
		return fmt.Errorf("isolate sandbox backend is unavailable: %w", err)
	}
	return nil
}

func developmentEnv(env string) bool {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "", "dev", "test", "local":
		return true
	default:
		return false
	}
}
