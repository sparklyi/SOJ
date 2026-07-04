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
