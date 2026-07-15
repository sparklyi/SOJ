package judgecore

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/judgecore/checker"
	"SOJ/internal/judgecore/language"
	"SOJ/internal/judgecore/sandbox"
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
		LanguageID: language.GoID,
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
		LanguageID: language.GoID,
		Source:     []byte("package main\nfunc main() {}\n"),
		Cases:      []Case{{Input: "", ExpectedOutput: ""}},
		Policy:     checker.PolicyExact,
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
		LanguageID: language.Cpp17ID,
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
		LanguageID: language.GoID,
		Source:     []byte(source),
		Cases:      cases,
		Policy:     checker.PolicyExact,
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
