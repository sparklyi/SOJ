package judgecore

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/judgecore/language"
)

// These tests execute real programs through the process sandbox. They are the
// reason Core.Run exists: before it, there was no way to say "compile this and
// run it once with this stdin", so self-runs never executed anything and came
// back as an empty accepted.

func TestRunGoEchoesStdin(t *testing.T) {
	requireGo(t)

	result, err := New(Options{}).Run(context.Background(), judge.RunRequest{
		LanguageID: language.GoID,
		Source: []byte(`package main
import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	fields := strings.Fields(line)
	sum := 0
	for _, field := range fields {
		n, _ := strconv.Atoi(field)
		sum += n
	}
	fmt.Println(sum)
}
`),
		Stdin:    "7 8 9\n",
		Timeout:  10 * time.Second,
		MemoryKB: 262144,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Verdict != judge.VerdictAccepted {
		t.Fatalf("verdict = %q, want accepted; result=%+v", result.Verdict, result)
	}
	if got := result.Stdout; got != "24\n" {
		t.Fatalf("stdout = %q, want %q: the program's output must reach the caller", got, "24\n")
	}
	if result.TimeMS <= 0 {
		t.Fatalf("time = %d, want a measured duration", result.TimeMS)
	}
	if result.Manifest.SandboxBackend != "process" {
		t.Fatalf("manifest = %+v", result.Manifest)
	}
}

func TestRunReturnsNothingForAnEmptyCaseList(t *testing.T) {
	// A run must not be Judge with zero cases. If Run were implemented as
	// "Judge with no testcases", this would report accepted with no output --
	// which is exactly the silent-success bug this operation was added to fix.
	requireGo(t)

	result, err := New(Options{}).Run(context.Background(), judge.RunRequest{
		LanguageID: language.GoID,
		Source: []byte(`package main
import "fmt"
func main() { fmt.Println("ran") }
`),
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Stdout != "ran\n" {
		t.Fatalf("stdout = %q, want %q", result.Stdout, "ran\n")
	}
	if len(result.Cases) != 0 {
		t.Fatalf("cases = %+v, want none: a run has nothing to compare", result.Cases)
	}
}

func TestRunReportsCompileError(t *testing.T) {
	requireGo(t)

	result, err := New(Options{}).Run(context.Background(), judge.RunRequest{
		LanguageID: language.GoID,
		Source:     []byte("package main\nfunc main() {\n"),
		Timeout:    10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Verdict != judge.VerdictCompileError {
		t.Fatalf("verdict = %q, want compile_error", result.Verdict)
	}
	if result.CompileOutput == "" {
		t.Fatal("CompileOutput is empty: the compiler's message must reach the user")
	}
}

func TestRunReportsNonZeroExitAsRuntimeError(t *testing.T) {
	requireGo(t)

	result, err := New(Options{}).Run(context.Background(), judge.RunRequest{
		LanguageID: language.GoID,
		Source: []byte(`package main
import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "boom")
	os.Exit(3)
}
`),
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Verdict != judge.VerdictRuntimeError {
		t.Fatalf("verdict = %q, want runtime_error", result.Verdict)
	}
	if result.Stderr == "" {
		t.Fatal("Stderr is empty: a crashed program's diagnostics must reach the user")
	}
}

func TestRunStopsAtTheTimeLimit(t *testing.T) {
	requireGo(t)

	result, err := New(Options{}).Run(context.Background(), judge.RunRequest{
		LanguageID: language.GoID,
		Source: []byte(`package main
func main() { for {} }
`),
		Timeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Verdict != judge.VerdictTimeLimit {
		t.Fatalf("verdict = %q, want time_limit", result.Verdict)
	}
}

func TestRunRejectsAnIncompleteRequest(t *testing.T) {
	core := New(Options{})

	for name, request := range map[string]judge.RunRequest{
		"no language": {Source: []byte("package main")},
		"no source":   {LanguageID: language.GoID},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := core.Run(context.Background(), request); err == nil {
				t.Fatal("Run returned no error for an incomplete request")
			}
		})
	}
}

func TestRunDoesNotRequireTestcases(t *testing.T) {
	// The counter-test to Judge: a judging request without testcases is invalid,
	// while a run request never carries any.
	if err := (Request{LanguageID: language.GoID, Source: []byte("x")}).Validate(); err == nil {
		t.Fatal("judging without testcases was accepted, want an error")
	}
	if err := (judge.RunRequest{LanguageID: language.GoID, Source: []byte("x")}).Validate(); err != nil {
		t.Fatalf("run request rejected: %v", err)
	}
}

func requireGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain is not available")
	}
}
