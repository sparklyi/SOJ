package judge

import (
	"context"
	"errors"
	"time"
)

type Verdict string

const (
	EngineSOJAgent       = "soj-agent"
	DefaultAgentEndpoint = "agent://local"
	AgentEndpointPrefix  = "agent://"
	// LocalEndpointPrefix selects an engine that executes scratch runs inside
	// the calling process instead of delegating to the judge-agent. It is for
	// single-node deployments and local development; see app.newRunEngine.
	LocalEndpointPrefix = "local://"
)

const (
	VerdictAccepted            Verdict = "accepted"
	VerdictWrongAnswer         Verdict = "wrong_answer"
	VerdictTimeLimitExceeded   Verdict = "time_limit_exceeded"
	VerdictMemoryLimitExceeded Verdict = "memory_limit_exceeded"
	VerdictTimeLimit           Verdict = "time_limit"
	VerdictMemoryLimit         Verdict = "memory_limit"
	VerdictOutputLimit         Verdict = "output_limit"
	VerdictRuntimeError        Verdict = "runtime_error"
	VerdictCompileError        Verdict = "compile_error"
	VerdictSystemError         Verdict = "system_error"
	VerdictCanceled            Verdict = "canceled"
)

type Testcase struct {
	InputKey          string
	ExpectedOutputKey string
	TimeLimit         time.Duration
	MemoryKB          int64
}

// Request is a testcase judging request. Testcases are named by storage key,
// not by content, so the engine resolves them against the object store.
type Request struct {
	LanguageSlug string
	Source       []byte
	// Stdin is carried for the agent wire protocol only (see AgentRequest).
	// Testcase judging does not use it: each Testcase brings its own input.
	// Scratch runs use RunRequest instead.
	Stdin     string
	Testcases []Testcase
	Timeout   time.Duration
}

// RunRequest is one execution of source with no expected output.
//
// It is deliberately not Request with an empty Testcases slice. Request asks
// "is this source correct", and its verdict is a statement about correctness;
// RunRequest asks only "did this source run", and has nothing to compare
// against. Sharing one type would make an empty testcase list silently mean
// "just run it", so a bug that fails to load testcases would come back as a
// clean Accepted instead of an error.
type RunRequest struct {
	LanguageSlug string
	Source       []byte
	Stdin        string
	Timeout      time.Duration
	MemoryKB     int64
}

// Validate reports whether the request is complete enough to execute. It lives
// here rather than in the executor because RunRequest owns its own invariants;
// every implementation would otherwise repeat the same three checks.
func (r RunRequest) Validate() error {
	if r.LanguageSlug == "" {
		return errors.New("language is required")
	}
	if len(r.Source) == 0 {
		return errors.New("source is required")
	}
	return nil
}

type Result struct {
	Verdict       Verdict
	TimeMS        int
	MemoryKB      int
	Stdout        string
	Stderr        string
	CompileOutput string
	ErrorMessage  string
	Cases         []CaseResult
	Manifest      Manifest
	JudgedAt      time.Time
}

type CaseResult struct {
	Index             int
	GroupName         string
	TestcaseKey       string
	Verdict           Verdict
	Score             int32
	TimeMS            int
	MemoryKB          int
	ExitCode          *int32
	Signal            string
	CheckerMessage    string
	OutputDiffSummary string
}

type Manifest struct {
	JudgeCoreVersion string
	JudgeAgentID     string
	LanguageRuntime  string
	SandboxBackend   string
	SandboxProfile   string
	TestcaseSetHash  string
	CheckerHash      string
	ValidatorHash    string
	TraceID          string
	Raw              map[string]any
}

// RunEngine executes scratch runs.
//
// It is a separate interface from JudgeEngine because the two capabilities are
// genuinely different, and a consumer should only be asked for the one it uses.
// A testcase judge has no business executing a run, and a run engine has no
// business implementing testcase comparison. The language catalog is not here
// either: only the catalog administration service asks for Languages.
type RunEngine interface {
	Run(ctx context.Context, request RunRequest) (Result, error)
}
