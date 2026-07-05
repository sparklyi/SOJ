package sandbox

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	BackendProcess = "process"
	BackendDocker  = "docker"
	BackendIsolate = "isolate"
	BackendFake    = "fake"
)

type Capabilities struct {
	Backend         string
	Profile         string
	Runtime         string
	ProductionReady bool
	UnsafeReason    string
}

func SelectBackend(env, configured, judgeEndpoint string) (string, error) {
	backend := strings.TrimSpace(configured)
	if backend == "" {
		if strings.HasPrefix(strings.TrimSpace(judgeEndpoint), "fake://") {
			backend = BackendFake
		} else if developmentEnv(env) {
			backend = BackendProcess
		} else {
			backend = BackendDocker
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
	case BackendProcess, BackendDocker, BackendIsolate, BackendFake:
		return backend, nil
	default:
		return "", fmt.Errorf("unsupported sandbox backend %q", backend)
	}
}

func ValidateProductionCapabilities(env string, capabilities Capabilities) error {
	if !productionEnv(env) {
		return nil
	}
	if capabilities.ProductionReady {
		return nil
	}
	reason := strings.TrimSpace(capabilities.UnsafeReason)
	if reason == "" {
		reason = fmt.Sprintf("%s sandbox backend is not production ready", capabilities.Backend)
	}
	return fmt.Errorf("unsafe sandbox capabilities for production: %s", reason)
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

func productionEnv(env string) bool {
	return strings.EqualFold(strings.TrimSpace(env), "prod")
}
