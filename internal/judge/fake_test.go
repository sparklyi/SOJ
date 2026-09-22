package judge

import (
	"context"
	"testing"
)

func TestFakeEngineRecordsRequestsAndReturnsQueuedResults(t *testing.T) {
	engine := NewFakeEngine(Result{Verdict: VerdictWrongAnswer, Stdout: "no"})

	result, err := engine.Judge(context.Background(), Request{LanguageID: 71, Source: []byte("package main")})
	if err != nil {
		t.Fatalf("Judge returned error: %v", err)
	}
	if result.Verdict != VerdictWrongAnswer {
		t.Fatalf("verdict = %q, want %q", result.Verdict, VerdictWrongAnswer)
	}
	requests := engine.Requests()
	if len(requests) != 1 || requests[0].LanguageID != 71 {
		t.Fatalf("requests = %+v, want one language 71 request", requests)
	}
}

func TestFakeEngineServesRunsFromTheSameScript(t *testing.T) {
	engine := NewFakeEngine(Result{Verdict: VerdictAccepted, Stdout: "42\n", TimeMS: 12})

	result, err := engine.Run(context.Background(), RunRequest{LanguageID: 71, Source: []byte("package main"), Stdin: "1 2\n"})
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

func TestFakeEngineRunValidatesTheRequest(t *testing.T) {
	engine := NewFakeEngine()

	if _, err := engine.Run(context.Background(), RunRequest{}); err == nil {
		t.Fatal("Run returned no error for an empty request")
	}
}

func TestUnavailableEngineRunNamesTheEndpoint(t *testing.T) {
	engine := NewUnavailableEngine("agent://local")

	_, err := engine.Run(context.Background(), RunRequest{LanguageID: 71, Source: []byte("package main")})
	if err == nil {
		t.Fatal("Run returned nil error, want an explicit failure")
	}
	if got := err.Error(); got != "judge endpoint agent://local is not implemented" {
		t.Fatalf("error = %q", got)
	}
}
