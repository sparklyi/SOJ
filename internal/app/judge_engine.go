package app

import (
	"net/http"
	"strings"

	"SOJ/internal/config"
	"SOJ/internal/judge"
)

func newJudgeEngine(cfg config.JudgeConfig) judge.JudgeEngine {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if strings.HasPrefix(endpoint, "fake://") {
		return fakeJudgeEngine(endpoint)
	}
	return judge.NewJudge0Client(endpoint, &http.Client{Timeout: cfg.Timeout}, "")
}

func fakeJudgeEngine(endpoint string) judge.JudgeEngine {
	engine := judge.NewFakeEngine()
	if strings.EqualFold(strings.TrimPrefix(endpoint, "fake://"), "accepted") {
		return engine
	}
	engine.SetError(errUnsupportedFakeJudge(endpoint))
	return engine
}

type errUnsupportedFakeJudge string

func (e errUnsupportedFakeJudge) Error() string {
	return "unsupported fake judge endpoint " + string(e)
}
