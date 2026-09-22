package app

import (
	"log/slog"
	"strings"
	"time"

	"SOJ/internal/config"
	"SOJ/internal/judge"
	"SOJ/internal/judgecore"
	"SOJ/internal/judgecore/sandbox"
)

// newJudgeEngine resolves the engine the API uses for the language catalog and
// for judging. Submissions are judged asynchronously by the judge-agent through
// Redis, so the API never actually calls Judge -- but the endpoint still has to
// answer Languages, which is how the catalog is seeded.
func newJudgeEngine(cfg config.JudgeConfig) judge.JudgeEngine {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = judge.DefaultAgentEndpoint
	}
	if strings.HasPrefix(endpoint, "fake://") {
		return fakeJudgeEngine(endpoint)
	}
	// Both agent:// and local:// delegate judging elsewhere (the judge-agent),
	// so this process cannot answer Judge. UnavailableEngine answers Languages
	// with an empty catalog rather than an error, which keeps the catalog sync
	// a no-op instead of a failure.
	if strings.HasPrefix(endpoint, judge.AgentEndpointPrefix) || strings.HasPrefix(endpoint, judge.LocalEndpointPrefix) {
		return judge.NewUnavailableEngine(endpoint)
	}
	return unsupportedJudgeEndpoint(endpoint)
}

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
func newRunEngine(cfg config.Config, logger *slog.Logger) (judge.RunEngine, error) {
	endpoint := strings.TrimSpace(cfg.Judge.Endpoint)
	if endpoint == "" {
		endpoint = judge.DefaultAgentEndpoint
	}
	switch {
	case strings.HasPrefix(endpoint, "fake://"):
		return fakeJudgeEngine(endpoint), nil
	case strings.HasPrefix(endpoint, judge.LocalEndpointPrefix):
		return newLocalRunEngine(cfg, logger)
	case strings.HasPrefix(endpoint, judge.AgentEndpointPrefix):
		return nil, nil
	default:
		return unsupportedJudgeEndpoint(endpoint), nil
	}
}

func newLocalRunEngine(cfg config.Config, logger *slog.Logger) (judge.RunEngine, error) {
	backend, err := sandbox.SelectBackend(cfg.Env, envOr("SOJ_JUDGE_SANDBOX_BACKEND", ""), cfg.Judge.Endpoint)
	if err != nil {
		return nil, err
	}
	if backend == sandbox.BackendDocker {
		return nil, errDockerBackendNotAllowedInAPI{}
	}
	if backend == sandbox.BackendFake {
		// fake:// is handled above; reaching here means the endpoint and the
		// backend disagree, and a fake run engine would silently return
		// accepted for code that never ran.
		return nil, errUnsupportedSandboxBackend(backend)
	}

	runner, err := newJudgeAgentSandbox(backend, cfg.Judge.CleanupTimeout, nil, logger)
	if err != nil {
		return nil, err
	}
	return judgecore.New(judgecore.Options{
		Sandbox:        runner,
		CleanupTimeout: cfg.Judge.CleanupTimeout,
	}), nil
}

// fakeJudgeEngine returns the concrete engine so callers can use it as either a
// JudgeEngine or a RunEngine without a type assertion.
func fakeJudgeEngine(endpoint string) *judge.FakeEngine {
	engine := judge.NewFakeEngine()
	if strings.EqualFold(strings.TrimPrefix(endpoint, "fake://"), "accepted") {
		engine.SetLanguages([]judge.Language{{
			ID:        71,
			Name:      "Fake Accepted",
			Enabled:   true,
			TimeLimit: time.Second,
			MemoryKB:  65536,
		}})
		return engine
	}
	engine.SetError(errUnsupportedFakeJudge(endpoint))
	return engine
}

type errUnsupportedFakeJudge string

func (e errUnsupportedFakeJudge) Error() string {
	return "unsupported fake judge endpoint " + string(e)
}

type errDockerBackendNotAllowedInAPI struct{}

func (errDockerBackendNotAllowedInAPI) Error() string {
	return "the docker sandbox backend cannot be used by the API process: " +
		"only soj-judge-agent may hold a Docker socket. Set SOJ_JUDGE_SANDBOX_BACKEND=isolate " +
		"or point SOJ_JUDGE_ENDPOINT at the agent instead"
}

func unsupportedJudgeEndpoint(endpoint string) *judge.FakeEngine {
	engine := judge.NewFakeEngine()
	engine.SetError(errUnsupportedJudgeEndpoint(endpoint))
	return engine
}

type errUnsupportedJudgeEndpoint string

func (e errUnsupportedJudgeEndpoint) Error() string {
	return "unsupported judge endpoint " + string(e)
}
