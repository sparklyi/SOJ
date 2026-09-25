package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"SOJ/internal/config"
	"SOJ/internal/judge"
	"SOJ/internal/judgecore/sandbox"
)

func TestNewJudgeEngineDefaultsToAgentProtocol(t *testing.T) {
	engine := newJudgeEngine(config.JudgeConfig{})

	languages, err := engine.Languages(context.Background())
	if err != nil {
		t.Fatalf("Languages returned error: %v", err)
	}
	if len(languages) != 0 {
		t.Fatalf("languages = %+v, want no baked-in languages from protocol stub", languages)
	}
	_, err = engine.Judge(context.Background(), judge.Request{LanguageID: 71, Source: []byte("package main")})
	if err == nil {
		t.Fatal("Judge returned nil error, want agent protocol unavailable until agent client is implemented")
	}
}

func TestFakeAcceptedJudgeEngineProvidesDefaultLanguage(t *testing.T) {
	engine := newJudgeEngine(config.JudgeConfig{Endpoint: "fake://accepted", Timeout: time.Second})

	languages, err := engine.Languages(context.Background())
	if err != nil {
		t.Fatalf("Languages returned error: %v", err)
	}
	if len(languages) != 1 {
		t.Fatalf("languages = %+v, want one fake language", languages)
	}
	if languages[0].ID != 71 || !languages[0].Enabled {
		t.Fatalf("language = %+v, want enabled fake language 71", languages[0])
	}
}

func TestNewJudgeEngineRejectsHTTPJudgeEndpoint(t *testing.T) {
	engine := newJudgeEngine(config.JudgeConfig{Endpoint: "http://legacy-judge:2358", Timeout: time.Second})

	_, err := engine.Languages(context.Background())
	if err == nil {
		t.Fatal("Languages returned nil error, want unsupported judge endpoint error")
	}
	if got, want := err.Error(), "unsupported judge endpoint http://legacy-judge:2358"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestNewWorkerObjectStorageAcceptsHTTPEndpoint(t *testing.T) {
	_, err := newWorkerObjectStorage(config.StorageConfig{
		Endpoint:     "http://minio:9000",
		Bucket:       "soj",
		AccessKey:    "minioadmin",
		SecretKey:    "minioadmin",
		UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("newWorkerObjectStorage returned error: %v", err)
	}
}

func TestNewJudgeAgentSandboxRejectsIsolateUntilAdapterExists(t *testing.T) {
	_, err := newJudgeAgentSandbox(sandbox.BackendIsolate, config.RunnerConfig{}, 0, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "isolate sandbox execution is not implemented") {
		t.Fatalf("err = %v, want explicit isolate unavailable error", err)
	}
}

func TestNewJudgeAgentSandboxAllowsProcessBackend(t *testing.T) {
	got, err := newJudgeAgentSandbox(sandbox.BackendProcess, config.RunnerConfig{}, 0, nil, nil)
	if err != nil {
		t.Fatalf("newJudgeAgentSandbox returned error: %v", err)
	}
	if got.Name() != sandbox.BackendProcess {
		t.Fatalf("backend = %q, want process", got.Name())
	}
}

func TestNewJudgeAgentSandboxAllowsDockerBackend(t *testing.T) {
	t.Setenv("SOJ_DOCKER_RUNNER_RUNTIME", "runsc")

	got, err := newJudgeAgentSandbox(sandbox.BackendDocker, config.RunnerConfig{}, 0, nil, nil)
	if err != nil {
		t.Fatalf("newJudgeAgentSandbox returned error: %v", err)
	}
	if got.Name() != sandbox.BackendDocker {
		t.Fatalf("backend = %q, want docker", got.Name())
	}
}

func TestNewRunEngineFakeEndpointRuns(t *testing.T) {
	engine, err := newRunEngine(config.Config{Judge: config.JudgeConfig{Endpoint: "fake://accepted"}}, nil)
	if err != nil {
		t.Fatalf("newRunEngine returned error: %v", err)
	}
	if _, err := engine.Run(context.Background(), judge.RunRequest{LanguageID: 71, Source: []byte("package main")}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

// TestNewRunEngineAgentEndpointHasNoInProcessEngine pins the production path:
// with agent:// the API process has no execution capability at all, and the nil
// engine is how RunService knows to enqueue the run instead of running it.
//
// Returning an engine that fails at call time was the old behaviour, and it made
// every production self-run a system_error. A nil engine that the service reads
// as "queued" is a decision; an engine that always errors is an accident.
func TestNewRunEngineAgentEndpointHasNoInProcessEngine(t *testing.T) {
	engine, err := newRunEngine(config.Config{Judge: config.JudgeConfig{Endpoint: "agent://local"}}, nil)
	if err != nil {
		t.Fatalf("newRunEngine returned error: %v", err)
	}
	if engine != nil {
		t.Fatalf("newRunEngine = %#v, want nil so runs are enqueued for the agent", engine)
	}
}

func TestNewRunEngineLocalEndpointUsesProcessSandboxInDevelopment(t *testing.T) {
	engine, err := newRunEngine(config.Config{Env: "local", Judge: config.JudgeConfig{Endpoint: "local://"}}, nil)
	if err != nil {
		t.Fatalf("newRunEngine returned error: %v", err)
	}
	if engine == nil {
		t.Fatal("engine is nil")
	}
}

// TestNewRunEngineLocalRefusesDockerBackend 是这条边界的关键断言：
// judge-agent 是唯一允许持有 Docker socket 的进程。为了一个练习场把 socket
// 交给 API，等于把这条边界拆掉。
func TestNewRunEngineLocalRefusesDockerBackend(t *testing.T) {
	t.Setenv("SOJ_JUDGE_SANDBOX_BACKEND", sandbox.BackendDocker)

	_, err := newRunEngine(config.Config{Env: "prod", Judge: config.JudgeConfig{Endpoint: "local://"}}, nil)
	if err == nil {
		t.Fatal("newRunEngine returned nil error, want the docker backend refused")
	}
	if !strings.Contains(err.Error(), "only soj-judge-agent may hold a Docker socket") {
		t.Fatalf("error = %q, want it to explain the boundary", err)
	}
}

func TestNewRunEngineLocalRejectsProcessSandboxOutsideDevelopment(t *testing.T) {
	t.Setenv("SOJ_JUDGE_SANDBOX_BACKEND", sandbox.BackendProcess)

	// SelectBackend 自己就会拦住「生产环境用 process 沙箱」，这里确认这条
	// 保护在 local:// 这条新路径上依然生效。
	if _, err := newRunEngine(config.Config{Env: "prod", Judge: config.JudgeConfig{Endpoint: "local://"}}, nil); err == nil {
		t.Fatal("newRunEngine returned nil error, want the process backend refused in prod")
	}
}

func TestNewJudgeEngineAcceptsLocalEndpointForLanguages(t *testing.T) {
	// local:// 不做判题，但必须能回答 Languages（空目录），否则语言目录同步
	// 会从一个可恢复的空状态变成一次失败。
	engine := newJudgeEngine(config.JudgeConfig{Endpoint: "local://"})
	if _, err := engine.Languages(context.Background()); err != nil {
		t.Fatalf("Languages returned error: %v", err)
	}
}
