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

func TestFakeEngineIsolatesRequestHistoryFromCallerAndSnapshotMutations(t *testing.T) {
	engine := NewFakeEngine(
		Result{Verdict: VerdictAccepted},
		Result{Verdict: VerdictWrongAnswer},
	)

	if got := engine.Requests(); got != nil {
		t.Fatalf("initial Requests() = %#v, want nil", got)
	}

	source := []byte("package main")
	testcases := []Testcase{{InputKey: "in/1", ExpectedOutputKey: "out/1", TimeLimit: time.Second, MemoryKB: 1024}}

	res1, err := engine.Judge(context.Background(), Request{
		LanguageSlug: "go",
		Source:       source,
		Testcases:    testcases,
	})
	if err != nil || res1.Verdict != VerdictAccepted {
		t.Fatalf("first Judge() = (%+v, %v), want Accepted", res1, err)
	}

	// Mutate caller-owned slices after Judge returns.
	source[0] = 'X'
	testcases[0].InputKey = "mutated-input"

	// Record a second request with nil slices to verify nil preservation and call order.
	res2, err := engine.Judge(context.Background(), Request{LanguageSlug: "python3"})
	if err != nil || res2.Verdict != VerdictWrongAnswer {
		t.Fatalf("second Judge() = (%+v, %v), want WrongAnswer", res2, err)
	}

	firstSnapshot := engine.Requests()
	if len(firstSnapshot) != 2 {
		t.Fatalf("len(Requests()) = %d, want 2", len(firstSnapshot))
	}
	if string(firstSnapshot[0].Source) != "package main" {
		t.Fatalf("recorded Source = %q, want original caller bytes", firstSnapshot[0].Source)
	}
	if len(firstSnapshot[0].Testcases) != 1 || firstSnapshot[0].Testcases[0].InputKey != "in/1" {
		t.Fatalf("recorded Testcases = %+v, want original testcase", firstSnapshot[0].Testcases)
	}
	if firstSnapshot[1].LanguageSlug != "python3" || firstSnapshot[1].Source != nil || firstSnapshot[1].Testcases != nil {
		t.Fatalf("second request = %+v, want python3 with nil slices preserved", firstSnapshot[1])
	}

	// Mutate slices returned by Requests() and confirm recorded history is unaffected.
	firstSnapshot[0].Source[0] = 'Y'
	firstSnapshot[0].Testcases[0].InputKey = "snapshot-mutated"

	secondSnapshot := engine.Requests()
	if string(secondSnapshot[0].Source) != "package main" {
		t.Fatalf("second snapshot Source = %q, want package main", secondSnapshot[0].Source)
	}
	if secondSnapshot[0].Testcases[0].InputKey != "in/1" {
		t.Fatalf("second snapshot Testcases = %+v, want original testcase", secondSnapshot[0].Testcases)
	}
}

func TestFakeEngineIsolatesRunRequestHistoryFromCallerAndSnapshotMutations(t *testing.T) {
	engine := NewFakeEngine(Result{Verdict: VerdictAccepted, Stdout: "ok"})

	if got := engine.RunRequests(); got != nil {
		t.Fatalf("initial RunRequests() = %#v, want nil", got)
	}

	source := []byte("print(1)")
	res, err := engine.Run(context.Background(), RunRequest{
		LanguageSlug: "python3",
		Source:       source,
		Stdin:        "input",
	})
	if err != nil || res.Stdout != "ok" {
		t.Fatalf("Run() = (%+v, %v), want ok", res, err)
	}

	// Mutate caller-owned source bytes.
	source[0] = 'X'

	firstSnapshot := engine.RunRequests()
	if len(firstSnapshot) != 1 || string(firstSnapshot[0].Source) != "print(1)" {
		t.Fatalf("recorded RunRequests() = %+v, want original source bytes", firstSnapshot)
	}

	// Mutate returned snapshot bytes.
	firstSnapshot[0].Source[0] = 'Y'

	secondSnapshot := engine.RunRequests()
	if string(secondSnapshot[0].Source) != "print(1)" {
		t.Fatalf("second snapshot RunRequests() = %+v, want original source bytes", secondSnapshot)
	}
}
