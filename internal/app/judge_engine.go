package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"SOJ/internal/config"
	"SOJ/internal/judge"
	"SOJ/internal/judgecore"
	"SOJ/internal/judgecore/sandbox"
	"SOJ/internal/language"
)

// newRunEngine resolves the engine that executes self-runs (the playground and
// the problem page's "run" button) inside this process.
//
// A nil engine is a real answer, not a failure: it means runs are enqueued for
// the judge-agent to execute, which is the production path. Only the agent holds
// a sandbox, so the API must not run untrusted code -- and now that runs travel
// the same async pipeline as submissions, it does not have to.
//
// local:// is the one endpoint that executes here. It exists for single-node
// deployments and local development, and it is deliberately narrow:
//
//   - it refuses the docker backend. The judge-agent is the only process meant
//     to hold a Docker socket; handing it to the API would dissolve that
//     boundary for the sake of a scratchpad. Use isolate or the agent instead.
//   - sandbox.SelectBackend already refuses the process backend outside
//     dev/test/local environments, so a production process cannot quietly end
//     up running untrusted code without a sandbox.
func newRunEngine(cfg config.Config, languages *language.Catalog, logger *slog.Logger) (judge.RunEngine, error) {
	endpoint := strings.TrimSpace(cfg.Judge.Endpoint)
	if endpoint == "" {
		endpoint = judge.DefaultAgentEndpoint
	}
	switch {
	case strings.HasPrefix(endpoint, "fake://"):
		return fakeJudgeEngine(endpoint), nil
	case strings.HasPrefix(endpoint, judge.LocalEndpointPrefix):
		return newLocalRunEngine(cfg, languages, logger)
	case strings.HasPrefix(endpoint, judge.AgentEndpointPrefix):
		return nil, nil
	default:
		return unsupportedJudgeEndpoint(endpoint), nil
	}
}

// localRunEngine executes self-runs in this process. The catalog translates the
// run's slug into the profile judgecore executes, which is why the core itself
// needs no catalog.
type localRunEngine struct {
	core      *judgecore.Core
	languages *language.Catalog
}

func (e localRunEngine) Run(ctx context.Context, request judge.RunRequest) (judge.Result, error) {
	profile, ok := e.languages.Lookup(request.LanguageSlug)
	if !ok {
		return judge.Result{}, fmt.Errorf("language %q is not configured", request.LanguageSlug)
	}
	return e.core.Run(ctx, judgecore.RunRequest{
		Language: profile,
		Source:   request.Source,
		Stdin:    request.Stdin,
		Timeout:  request.Timeout,
		MemoryKB: request.MemoryKB,
	})
}

func newLocalRunEngine(cfg config.Config, languages *language.Catalog, logger *slog.Logger) (judge.RunEngine, error) {
	backend, err := sandbox.SelectBackend(cfg.Env, cfg.Judge.SandboxBackend, cfg.Judge.Endpoint)
	if err != nil {
		return nil, err
	}
	if backend == sandbox.BackendDocker {
		return nil, errDockerBackendNotAllowedInAPI
	}
	if backend == sandbox.BackendFake {
		// fake:// is handled above; reaching here means the endpoint and the
		// backend disagree, and a fake run engine would silently return
		// accepted for code that never ran.
		return nil, unsupportedSandboxBackendError(backend)
	}

	runner, err := newJudgeAgentSandbox(backend, cfg.Agent.Runner, "", cfg.Judge.CleanupTimeout, nil, logger)
	if err != nil {
		return nil, err
	}
	return localRunEngine{
		core:      judgecore.New(judgecore.Options{Sandbox: runner, CleanupTimeout: cfg.Judge.CleanupTimeout}),
		languages: languages,
	}, nil
}

// inlineJudgeEngine is the judging capability the worker's inline path needs.
// It lives here, next to its only caller, rather than in the judge package.
type inlineJudgeEngine interface {
	Judge(ctx context.Context, request judge.Request) (judge.Result, error)
}

// workerJudgeEngine resolves the engine behind the worker's inline judging
// path. In production it is unavailable on purpose: judging happens on the
// judge agent, and an engine that says so beats one that silently accepts.
func workerJudgeEngine(cfg config.JudgeConfig) inlineJudgeEngine {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = judge.DefaultAgentEndpoint
	}
	if strings.HasPrefix(endpoint, "fake://") {
		return fakeJudgeEngine(endpoint)
	}
	return judge.NewUnavailableEngine(endpoint)
}

// fakeJudgeEngine returns the concrete engine so callers can use it as either a
// JudgeEngine or a RunEngine without a type assertion.
func fakeJudgeEngine(endpoint string) *judge.FakeEngine {
	engine := judge.NewFakeEngine()
	if !strings.EqualFold(strings.TrimPrefix(endpoint, "fake://"), "accepted") {
		engine.SetError(unsupportedFakeJudgeError(endpoint))
	}
	return engine
}

type unsupportedFakeJudgeError string

func (e unsupportedFakeJudgeError) Error() string {
	return "unsupported fake judge endpoint " + string(e)
}

// errDockerBackendNotAllowedInAPI keeps the docker socket out of the API
// process: only soj-judge-agent is allowed to hold it.
var errDockerBackendNotAllowedInAPI = errors.New("the docker sandbox backend cannot be used by the API process: " +
	"only soj-judge-agent may hold a Docker socket. Set SOJ_JUDGE_SANDBOX_BACKEND=isolate " +
	"or point SOJ_JUDGE_ENDPOINT at the agent instead")

func unsupportedJudgeEndpoint(endpoint string) *judge.FakeEngine {
	engine := judge.NewFakeEngine()
	engine.SetError(unsupportedJudgeEndpointError(endpoint))
	return engine
}

type unsupportedJudgeEndpointError string

func (e unsupportedJudgeEndpointError) Error() string {
	return "unsupported judge endpoint " + string(e)
}
