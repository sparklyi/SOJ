package judge

import (
	"context"
	"sync"
	"time"
)

type FakeEngine struct {
	mu        sync.Mutex
	results   []Result
	err       error
	languages []Language
	requests  []Request
	delay     time.Duration
}

func NewFakeEngine(results ...Result) *FakeEngine {
	return &FakeEngine{results: append([]Result(nil), results...)}
}

func (e *FakeEngine) SetError(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.err = err
}

func (e *FakeEngine) SetLanguages(languages []Language) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.languages = append([]Language(nil), languages...)
}

func (e *FakeEngine) SetDelay(delay time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.delay = delay
}

func (e *FakeEngine) Requests() []Request {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Request(nil), e.requests...)
}

func (e *FakeEngine) Judge(ctx context.Context, request Request) (Result, error) {
	e.mu.Lock()
	delay := e.delay
	e.requests = append(e.requests, request)
	e.mu.Unlock()

	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Result{}, ctx.Err()
		case <-timer.C:
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return Result{}, e.err
	}
	if len(e.results) == 0 {
		return Result{Verdict: VerdictAccepted, JudgedAt: time.Now().UTC()}, nil
	}
	result := e.results[0]
	e.results = e.results[1:]
	if result.JudgedAt.IsZero() {
		result.JudgedAt = time.Now().UTC()
	}
	return result, nil
}

func (e *FakeEngine) Languages(ctx context.Context) ([]Language, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return nil, e.err
	}
	return append([]Language(nil), e.languages...), nil
}
