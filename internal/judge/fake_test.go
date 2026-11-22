package judge

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFakeEngineRecordsRequestsAndReturnsQueuedResults(t *testing.T) {
	engine := NewFakeEngine(Result{Verdict: VerdictWrongAnswer, Stdout: "no"})

	result, err := engine.Judge(context.Background(), Request{LanguageSlug: "go", Source: []byte("package main")})
	if err != nil {
		t.Fatalf("Judge returned error: %v", err)
	}
	if result.Verdict != VerdictWrongAnswer {
		t.Fatalf("verdict = %q, want %q", result.Verdict, VerdictWrongAnswer)
	}
	requests := engine.Requests()
	if len(requests) != 1 || requests[0].LanguageSlug != "go" {
		t.Fatalf("requests = %+v, want one go request", requests)
	}
}

func TestFakeEngineServesRunsFromTheSameScript(t *testing.T) {
	engine := NewFakeEngine(Result{Verdict: VerdictAccepted, Stdout: "42\n", TimeMS: 12})

	result, err := engine.Run(context.Background(), RunRequest{LanguageSlug: "go", Source: []byte("package main"), Stdin: "1 2\n"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Stdout != "42\n" || result.TimeMS != 12 {
		t.Fatalf("result = %+v, want the scripted result", result)
	}

	// Runs are recorded apart from judging requests, so a test can say which
	// operation it observed.
	if judging := engine.Requests(); len(judging) != 0 {
		t.Fatalf("judging requests = %+v, want none", judging)
	}
	runs := engine.RunRequests()
	if len(runs) != 1 || runs[0].Stdin != "1 2\n" {
		t.Fatalf("run requests = %+v, want one with the stdin", runs)
	}
}

func TestFakeEngineCanceledContextDoesNotConsumeResult(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, expire := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expire()

	contexts := map[string]context.Context{"canceled": canceled, "expired": expired}
	for name, dead := range contexts {
		t.Run("Judge/"+name, func(t *testing.T) {
			engine := NewFakeEngine(Result{Verdict: VerdictWrongAnswer, Stdout: "no"})

			if _, err := engine.Judge(dead, Request{LanguageSlug: "go", Source: []byte("package main")}); !errors.Is(err, dead.Err()) {
				t.Fatalf("first call error = %v, want %v", err, dead.Err())
			}
			result, err := engine.Judge(context.Background(), Request{LanguageSlug: "go", Source: []byte("package main")})
			if err != nil {
				t.Fatalf("second call returned error: %v", err)
			}
			if result.Verdict != VerdictWrongAnswer {
				t.Fatalf("second call verdict = %q, want %q", result.Verdict, VerdictWrongAnswer)
			}
		})
		t.Run("Run/"+name, func(t *testing.T) {
			engine := NewFakeEngine(Result{Verdict: VerdictWrongAnswer, Stdout: "no"})

			if _, err := engine.Run(dead, RunRequest{LanguageSlug: "go", Source: []byte("package main")}); !errors.Is(err, dead.Err()) {
				t.Fatalf("first call error = %v, want %v", err, dead.Err())
			}
			result, err := engine.Run(context.Background(), RunRequest{LanguageSlug: "go", Source: []byte("package main")})
			if err != nil {
				t.Fatalf("second call returned error: %v", err)
			}
			if result.Verdict != VerdictWrongAnswer {
				t.Fatalf("second call verdict = %q, want %q", result.Verdict, VerdictWrongAnswer)
			}
		})
	}
}

func TestFakeEngineRunValidatesTheRequest(t *testing.T) {
	engine := NewFakeEngine()

	if _, err := engine.Run(context.Background(), RunRequest{}); err == nil {
		t.Fatal("Run returned no error for an empty request")
	}
}

func TestUnavailableEngineRunNamesTheEndpoint(t *testing.T) {
	engine := NewUnavailableEngine("agent://local")

	_, err := engine.Run(context.Background(), RunRequest{LanguageSlug: "go", Source: []byte("package main")})
	if err == nil {
		t.Fatal("Run returned nil error, want an explicit failure")
	}
	if got := err.Error(); got != "judge endpoint agent://local is not implemented" {
		t.Fatalf("error = %q", got)
	}
}
