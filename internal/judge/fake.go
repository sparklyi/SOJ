package judge

import (
	"context"
	"slices"
	"sync"
	"time"
)

type FakeEngine struct {
	mu          sync.Mutex
	results     []Result
	err         error
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

func (e *FakeEngine) SetDelay(delay time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.delay = delay
}

// Requests returns every judging request the engine has seen.
func (e *FakeEngine) Requests() []Request {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.requests) == 0 {
		return nil
	}
	requests := make([]Request, len(e.requests))
	for i, request := range e.requests {
		requests[i] = cloneRequest(request)
	}
	return requests
}

// RunRequests returns every scratch run the engine has seen. Kept separate from
// Requests because the two are different operations; folding them into one
// slice would make a test unable to say which one it observed.
func (e *FakeEngine) RunRequests() []RunRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.runRequests) == 0 {
		return nil
	}
	requests := make([]RunRequest, len(e.runRequests))
	for i, request := range e.runRequests {
		requests[i] = cloneRunRequest(request)
	}
	return requests
}

func (e *FakeEngine) Judge(ctx context.Context, request Request) (Result, error) {
	e.mu.Lock()
	e.requests = append(e.requests, cloneRequest(request))
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
	e.runRequests = append(e.runRequests, cloneRunRequest(request))
	e.mu.Unlock()

	return e.serve(ctx)
}

func cloneRequest(request Request) Request {
	request.Source = slices.Clone(request.Source)
	request.Testcases = slices.Clone(request.Testcases)
	return request
}

func cloneRunRequest(request RunRequest) RunRequest {
	request.Source = slices.Clone(request.Source)
	return request
}

// serve applies the configured delay and returns the next scripted result.
// Shared so Judge and Run cannot drift on how results are scripted.
func (e *FakeEngine) serve(ctx context.Context) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

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
