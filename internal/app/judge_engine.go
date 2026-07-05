package app

import (
	"strings"
	"time"

	"SOJ/internal/config"
	"SOJ/internal/judge"
)

func newJudgeEngine(cfg config.JudgeConfig) judge.JudgeEngine {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = judge.DefaultAgentEndpoint
	}
	if strings.HasPrefix(endpoint, "fake://") {
		return fakeJudgeEngine(endpoint)
	}
	if strings.HasPrefix(endpoint, judge.AgentEndpointPrefix) {
		return judge.NewUnavailableEngine(endpoint)
	}
	return unsupportedJudgeEndpoint(endpoint)
}

func fakeJudgeEngine(endpoint string) judge.JudgeEngine {
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

func unsupportedJudgeEndpoint(endpoint string) judge.JudgeEngine {
	engine := judge.NewFakeEngine()
	engine.SetError(errUnsupportedJudgeEndpoint(endpoint))
	return engine
}

type errUnsupportedJudgeEndpoint string

func (e errUnsupportedJudgeEndpoint) Error() string {
	return "unsupported judge endpoint " + string(e)
}
