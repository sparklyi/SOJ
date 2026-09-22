package judge

import (
	"context"
	"sync"
	"time"
)

type FakeEngine struct {
	mu          sync.Mutex
	results     []Result
	err         error
	languages   []Language
	requests    []Request
	runRequests []RunRequest
	delay       time.Duration
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

// Requests returns every judging request the engine has seen.
func (e *FakeEngine) Requests() []Request {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Request(nil), e.requests...)
}

// RunRequests returns every scratch run the engine has seen. Kept separate from
// Requests because the two are different operations; folding them into one
// slice would make a test unable to say which one it observed.
func (e *FakeEngine) RunRequests() []RunRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]RunRequest(nil), e.runRequests...)
}

func (e *FakeEngine) Judge(ctx context.Context, request Request) (Result, error) {
	e.mu.Lock()
	e.requests = append(e.requests, request)
	e.mu.Unlock()

	return e.serve(ctx)
}

// Run serves the next scripted result, exactly like Judge. A scratch run and a
// judged submission differ in what the caller does with the result, not in how
// a fake produces one.
func (e *FakeEngine) Run(ctx context.Context, request RunRequest) (Result, error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}

	e.mu.Lock()
	e.runRequests = append(e.runRequests, request)
	e.mu.Unlock()

	return e.serve(ctx)
}

// serve applies the configured delay and returns the next scripted result.
// Shared so Judge and Run cannot drift on how results are scripted.
func (e *FakeEngine) serve(ctx context.Context) (Result, error) {
	e.mu.Lock()
	delay := e.delay
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
