package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type judgeAgentSlotLimiter struct {
	global    chan struct{}
	languages map[string]chan struct{}
}

func newJudgeAgentSlotLimiter(global int, languages map[string]int) *judgeAgentSlotLimiter {
	if global <= 0 {
		global = 1
	}
	limiter := &judgeAgentSlotLimiter{
		global:    make(chan struct{}, global),
		languages: make(map[string]chan struct{}, len(languages)),
	}
	for language, slots := range languages {
		key := normalizeJudgeAgentLanguageKey(language)
		if key == "" || slots <= 0 {
			continue
		}
		limiter.languages[key] = make(chan struct{}, slots)
	}
	return limiter
}

func (l *judgeAgentSlotLimiter) Available() int {
	if l == nil {
		return 1
	}
	return cap(l.global) - len(l.global)
}

func (l *judgeAgentSlotLimiter) Acquire(ctx context.Context, language string) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	select {
	case l.global <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	languageSlots := l.languages[normalizeJudgeAgentLanguageKey(language)]
	if languageSlots == nil {
		return l.release(nil), nil
	}
	select {
	case languageSlots <- struct{}{}:
		return l.release(languageSlots), nil
	case <-ctx.Done():
		<-l.global
		return nil, ctx.Err()
	}
}

func (l *judgeAgentSlotLimiter) release(languageSlots chan struct{}) func() {
	var released bool
	return func() {
		if released {
			return
		}
		released = true
		if languageSlots != nil {
			<-languageSlots
		}
		<-l.global
	}
}

func parseJudgeAgentLanguageSlots(value string) (map[string]int, error) {
	out := map[string]int{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, raw, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("invalid language slot %q: want language=count", item)
		}
		language := normalizeJudgeAgentLanguageKey(key)
		if language == "" {
			return nil, fmt.Errorf("invalid language slot %q: language is required", item)
		}
		count, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || count <= 0 {
			return nil, fmt.Errorf("invalid language slot %q: count must be positive", item)
		}
		out[language] = count
	}
	return out, nil
}

func normalizeJudgeAgentLanguageKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
