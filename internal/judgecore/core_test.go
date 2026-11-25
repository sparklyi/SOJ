package judgecore

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/judgecore/checker"
	"SOJ/internal/judgecore/sandbox"
	"SOJ/internal/language"
)

const normalCaseTimeLimit = 10 * time.Second

func TestCoreJudgesGoAccepted(t *testing.T) {
	result := judgeGo(t, `package main
import "fmt"
func main() { var a, b int; fmt.Scan(&a, &b); fmt.Println(a + b) }
`, []Case{{Input: "1 2\n", ExpectedOutput: "3\n", TimeLimit: normalCaseTimeLimit}})

	if result.Verdict != judge.VerdictAccepted {
		t.Fatalf("verdict = %q, want accepted; result=%+v", result.Verdict, result)
	}
	if len(result.Cases) != 1 || result.Cases[0].Verdict != judge.VerdictAccepted {
		t.Fatalf("cases = %+v", result.Cases)
	}
	if result.Manifest.JudgeCoreVersion != Version || result.Manifest.SandboxBackend != "process" {
		t.Fatalf("manifest = %+v", result.Manifest)
	}
}

func TestCoreJudgesGoWrongAnswer(t *testing.T) {
	result := judgeGo(t, `package main
import "fmt"
func main() { fmt.Println(41) }
`, []Case{{Input: "", ExpectedOutput: "42\n", TimeLimit: normalCaseTimeLimit}})

	if result.Verdict != judge.VerdictWrongAnswer {
		t.Fatalf("verdict = %q, want wrong_answer; result=%+v", result.Verdict, result)
	}
	if result.Cases[0].CheckerMessage == "" || result.Cases[0].OutputDiffSummary == "" {
		t.Fatalf("case missing checker diagnostics: %+v", result.Cases[0])
	}
}

func TestCoreJudgesGoCompileError(t *testing.T) {
	result := judgeGo(t, `package main
func main() {
`, []Case{{Input: "", ExpectedOutput: "", TimeLimit: normalCaseTimeLimit}})

	if result.Verdict != judge.VerdictCompileError {
		t.Fatalf("verdict = %q, want compile_error; output=%q", result.Verdict, result.CompileOutput)
	}
	if result.CompileOutput == "" {
		t.Fatal("CompileOutput is empty")
	}
}

func TestCoreJudgesGoTimeLimit(t *testing.T) {
	result := judgeGo(t, `package main
func main() { for {} }
`, []Case{{Input: "", ExpectedOutput: "", TimeLimit: 100 * time.Millisecond}})

	if result.Verdict != judge.VerdictTimeLimit {
		t.Fatalf("verdict = %q, want time_limit; result=%+v", result.Verdict, result)
	}
}

func TestCoreJudgesGoOutputLimit(t *testing.T) {
	core := New(Options{})
	result, err := core.Judge(context.Background(), Request{
		Language: testGoProfile(),
		Source: []byte(`package main
import "fmt"
func main() { for i := 0; i < 4096; i++ { fmt.Print("x") } }
`),
		Cases:            []Case{{Input: "", ExpectedOutput: "", TimeLimit: normalCaseTimeLimit}},
		Policy:           checker.PolicyExact,
		OutputLimitBytes: 1024,
	})
	if err != nil {
		t.Fatalf("Judge returned error: %v", err)
	}
	if result.Verdict != judge.VerdictOutputLimit {
		t.Fatalf("verdict = %q, want output_limit; result=%+v", result.Verdict, result)
	}
}

func TestCoreCleanupUsesConfiguredDeadline(t *testing.T) {
	cleanupTimeout := 25 * time.Millisecond
	backend := &cleanupDeadlineSandbox{}
	core := New(Options{Sandbox: backend, CleanupTimeout: cleanupTimeout})

	started := time.Now()
	result, err := core.Judge(context.Background(), Request{
		Language: testGoProfile(),
		Source:   []byte("package main\nfunc main() {}\n"),
		Cases:    []Case{{Input: "", ExpectedOutput: ""}},
		Policy:   checker.PolicyExact,
	})
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("Judge returned error: %v", err)
	}
	if result.Verdict != judge.VerdictCompileError {
		t.Fatalf("verdict = %q, want compile_error", result.Verdict)
	}
	if !backend.cleanupSawDeadline {
		t.Fatal("Cleanup context did not have a deadline")
	}
	if elapsed < cleanupTimeout/2 {
		t.Fatalf("Judge returned after %v, want cleanup to wait for its %v deadline", elapsed, cleanupTimeout)
	}
	if elapsed > time.Second {
		t.Fatalf("Judge returned after %v, cleanup deadline was not enforced", elapsed)
	}
}

func TestCoreJudgesCpp17AcceptedWhenCompilerExists(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ is not available")
	}
	core := New(Options{})
	result, err := core.Judge(context.Background(), Request{
		Language: testCpp17Profile(),
		Source: []byte(`#include <iostream>
int main() { int a, b; std::cin >> a >> b; std::cout << a + b << "\n"; }
`),
		Cases:  []Case{{Input: "2 5\n", ExpectedOutput: "7\n", TimeLimit: normalCaseTimeLimit}},
		Policy: checker.PolicyExact,
	})
	if err != nil {
		t.Fatalf("Judge returned error: %v", err)
	}
	if result.Verdict != judge.VerdictAccepted {
		t.Fatalf("verdict = %q, want accepted; result=%+v", result.Verdict, result)
	}
}

func judgeGo(t *testing.T, source string, cases []Case) judge.Result {
	t.Helper()
	core := New(Options{})
	result, err := core.Judge(context.Background(), Request{
		Language: testGoProfile(),
		Source:   []byte(source),
		Cases:    cases,
		Policy:   checker.PolicyExact,
	})
	if err != nil {
		t.Fatalf("Judge returned error: %v", err)
	}
	return result
}

type cleanupDeadlineSandbox struct {
	cleanupSawDeadline bool
}

func (s *cleanupDeadlineSandbox) Name() string { return "cleanup-deadline" }

func (s *cleanupDeadlineSandbox) Profile() string { return "test" }

func (s *cleanupDeadlineSandbox) Probe(context.Context) (sandbox.Capabilities, error) {
	return sandbox.Capabilities{}, nil
}

func (s *cleanupDeadlineSandbox) Prepare(context.Context, sandbox.PrepareRequest) (sandbox.Workspace, error) {
	return sandbox.Workspace{}, nil
}

func (s *cleanupDeadlineSandbox) Compile(context.Context, sandbox.Workspace, language.Profile) (sandbox.CompileResult, error) {
	return sandbox.CompileResult{Verdict: judge.VerdictCompileError}, nil
}

func (s *cleanupDeadlineSandbox) Run(context.Context, sandbox.Workspace, language.Profile, sandbox.RunRequest) (sandbox.RunResult, error) {
	return sandbox.RunResult{}, nil
}

func (s *cleanupDeadlineSandbox) Cleanup(ctx context.Context, _ sandbox.Workspace) error {
	if _, ok := ctx.Deadline(); !ok {
		return nil
	}
	s.cleanupSawDeadline = true
	<-ctx.Done()
	return ctx.Err()
}

// scriptedSandbox returns the configured compile result and replays one
// RunResult per Run call, in order, recording the requests it saw.
type scriptedSandbox struct {
	compiled sandbox.CompileResult
	runs     []sandbox.RunResult
	requests []sandbox.RunRequest
	cleanups int
}

func (s *scriptedSandbox) Name() string    { return "scripted" }
func (s *scriptedSandbox) Profile() string { return "test" }

func (s *scriptedSandbox) Probe(context.Context) (sandbox.Capabilities, error) {
	return sandbox.Capabilities{}, nil
}

func (s *scriptedSandbox) Prepare(context.Context, sandbox.PrepareRequest) (sandbox.Workspace, error) {
	return sandbox.Workspace{}, nil
}

func (s *scriptedSandbox) Compile(context.Context, sandbox.Workspace, language.Profile) (sandbox.CompileResult, error) {
	return s.compiled, nil
}

func (s *scriptedSandbox) Run(_ context.Context, _ sandbox.Workspace, _ language.Profile, request sandbox.RunRequest) (sandbox.RunResult, error) {
	s.requests = append(s.requests, request)
	if len(s.requests) > len(s.runs) {
		return sandbox.RunResult{}, fmt.Errorf("unexpected extra Run call #%d", len(s.requests))
	}
	return s.runs[len(s.requests)-1], nil
}

func (s *scriptedSandbox) Cleanup(context.Context, sandbox.Workspace) error {
	s.cleanups++
	return nil
}

// scriptedChecker replays one Result per Check call and records requests.
type scriptedChecker struct {
	results  []checker.Result
	requests []checker.Request
}

func (c *scriptedChecker) Check(_ context.Context, request checker.Request) (checker.Result, error) {
	c.requests = append(c.requests, request)
	if len(c.requests) > len(c.results) {
		return checker.Result{}, fmt.Errorf("unexpected extra Check call #%d", len(c.requests))
	}
	return c.results[len(c.requests)-1], nil
}

func TestCorePreservesCompileVerdict(t *testing.T) {
	profile := testGoProfile()
	source := []byte("package main\nfunc main() {}\n")
	entries := []struct {
		name string
		call func(*Core) (judge.Result, error)
	}{
		{"Judge", func(core *Core) (judge.Result, error) {
			return core.Judge(t.Context(), Request{
				Language: profile, Source: source, Cases: []Case{{Input: "1\n", ExpectedOutput: "2\n"}},
			})
		}},
		{"Run", func(core *Core) (judge.Result, error) {
			return core.Run(t.Context(), RunRequest{Language: profile, Source: source, Stdin: "1\n"})
		}},
	}
	for _, entry := range entries {
		for _, compiled := range []sandbox.CompileResult{
			{Verdict: judge.VerdictCompileError, Output: "main.go:3: undefined: value", ErrorMessage: "compilation failed"},
			{Verdict: judge.VerdictSystemError, Output: "toolchain unavailable", ErrorMessage: "compiler could not start"},
		} {
			t.Run(entry.name+"/"+string(compiled.Verdict), func(t *testing.T) {
				backend := &scriptedSandbox{compiled: compiled}
				outputChecker := &scriptedChecker{}
				got, err := entry.call(New(Options{Sandbox: backend, Checker: outputChecker}))
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if got.Verdict != compiled.Verdict || got.CompileOutput != compiled.Output || got.ErrorMessage != compiled.ErrorMessage {
					t.Errorf("got verdict %q, output %q, message %q; want %+v", got.Verdict, got.CompileOutput, got.ErrorMessage, compiled)
				}
				if len(backend.requests) != 0 || len(outputChecker.requests) != 0 {
					t.Errorf("got %d run calls and %d checker calls after compilation failed", len(backend.requests), len(outputChecker.requests))
				}
			})
		}
	}
}

func TestCoreAggregatesCaseResults(t *testing.T) {
	backend := &scriptedSandbox{compiled: sandbox.CompileResult{Verdict: judge.VerdictAccepted}, runs: []sandbox.RunResult{
		// Case 1: executes fine, checker accepts. Memory maximum lives here.
		{Verdict: judge.VerdictAccepted, Stdout: "3\n", TimeMS: 10, MemoryKB: 900},
		// Case 2: executes fine, checker rejects -> wrong_answer. Time maximum lives here.
		{Verdict: judge.VerdictAccepted, Stdout: "4\n", TimeMS: 50, MemoryKB: 256},
		// Case 3: execution itself fails later with a different verdict.
		{Verdict: judge.VerdictTimeLimit, TimeMS: 30, MemoryKB: 64, ErrorMessage: "time limit exceeded"},
	}}
	outputChecker := &scriptedChecker{results: []checker.Result{
		{Accepted: true},
		{Accepted: false, Message: "output differs from expected output", DiffSummary: `expected "3\n", got "4\n"`},
	}}
	judgedAt := time.Unix(1_700_000_000, 0).UTC()
	core := New(Options{Sandbox: backend, Checker: outputChecker, Now: func() time.Time { return judgedAt }})

	result, err := core.Judge(context.Background(), Request{
		Language: testGoProfile(),
		Source:   []byte("package main\nfunc main() {}\n"),
		Cases: []Case{
			{Index: 1, Input: "1 2\n", ExpectedOutput: "3\n", TimeLimit: normalCaseTimeLimit, Score: 30},
			{Index: 2, Input: "1 3\n", ExpectedOutput: "4\n", TimeLimit: normalCaseTimeLimit, Score: 40},
			{Index: 3, Input: "", ExpectedOutput: "", TimeLimit: normalCaseTimeLimit, Score: 30},
		},
		Policy: checker.PolicyExact,
	})
	if err != nil {
		t.Fatalf("Judge returned error: %v", err)
	}

	// The aggregate verdict is the FIRST unsuccessful case verdict (case 2's
	// wrong_answer), not the later time_limit and not accepted.
	if result.Verdict != judge.VerdictWrongAnswer {
		t.Fatalf("aggregate verdict = %q, want wrong_answer; result=%+v", result.Verdict, result)
	}
	// Totals are per-field maxima, not sums (90 ms / 1220 KB) and not the last
	// case's values (30 ms / 64 KB); the maxima sit on different cases.
	if result.TimeMS != 50 {
		t.Fatalf("aggregate TimeMS = %d, want max 50", result.TimeMS)
	}
	if result.MemoryKB != 900 {
		t.Fatalf("aggregate MemoryKB = %d, want max 900", result.MemoryKB)
	}
	// Every case stays present, in order, with its own verdict.
	wantVerdicts := []judge.Verdict{judge.VerdictAccepted, judge.VerdictWrongAnswer, judge.VerdictTimeLimit}
	if len(result.Cases) != len(wantVerdicts) {
		t.Fatalf("cases = %+v, want %d entries", result.Cases, len(wantVerdicts))
	}
	for i, want := range wantVerdicts {
		got := result.Cases[i]
		if got.Index != i+1 || got.Verdict != want {
			t.Fatalf("case %d = {Index:%d Verdict:%q}, want {Index:%d Verdict:%q}", i, got.Index, got.Verdict, i+1, want)
		}
	}
	// Scores: only accepted cases score.
	wantScores := []int32{30, 0, 0}
	for i, want := range wantScores {
		if result.Cases[i].Score != want {
			t.Fatalf("case %d score = %d, want %d", i+1, result.Cases[i].Score, want)
		}
	}
	// Diagnostics: the rejected comparison carries checker output, the failed
	// execution carries the runner's error message.
	if result.Cases[1].CheckerMessage == "" || result.Cases[1].OutputDiffSummary == "" {
		t.Fatalf("case 2 missing checker diagnostics: %+v", result.Cases[1])
	}
	if result.Cases[2].CheckerMessage != "time limit exceeded" {
		t.Fatalf("case 3 CheckerMessage = %q, want the run error message", result.Cases[2].CheckerMessage)
	}
	// The checker runs only for successful executions: cases 1 and 2, never the
	// time-limited case 3.
	if len(outputChecker.requests) != 2 {
		t.Fatalf("checker called %d times, want 2 (accepted executions only)", len(outputChecker.requests))
	}
	if outputChecker.requests[0].Expected != "3\n" || outputChecker.requests[0].Actual != "3\n" {
		t.Fatalf("checker call 1 = %+v", outputChecker.requests[0])
	}
	if outputChecker.requests[1].Expected != "4\n" || outputChecker.requests[1].Actual != "4\n" {
		t.Fatalf("checker call 2 = %+v", outputChecker.requests[1])
	}
	// Each case ran with its own stdin, in order.
	if len(backend.requests) != 3 || backend.requests[0].Stdin != "1 2\n" || backend.requests[1].Stdin != "1 3\n" || backend.requests[2].Stdin != "" {
		t.Fatalf("run requests = %+v", backend.requests)
	}
	if !result.JudgedAt.Equal(judgedAt) {
		t.Fatalf("JudgedAt = %v, want %v", result.JudgedAt, judgedAt)
	}
	if backend.cleanups != 1 {
		t.Fatalf("cleanups = %d, want 1", backend.cleanups)
	}
}
