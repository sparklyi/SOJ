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

// The json tags below are the `judge.result.v1` wire contract, not a style
// choice. Result travels embedded in judgeevents.ResultEvent over Redis Streams
// between the worker and the judge agent, and without an explicit tag the wire
// name would be the Go field name -- so renaming TimeMS to TimeMs would silently
// rename a protocol field and a mixed-version deploy would read zeroes. Keep
// these names stable and change them only with a protocol version bump.
type Result struct {
	Verdict       Verdict      `json:"Verdict"`
	TimeMS        int          `json:"TimeMS"`
	MemoryKB      int          `json:"MemoryKB"`
	Stdout        string       `json:"Stdout"`
	Stderr        string       `json:"Stderr"`
	CompileOutput string       `json:"CompileOutput"`
	ErrorMessage  string       `json:"ErrorMessage"`
	Cases         []CaseResult `json:"Cases"`
	Manifest      Manifest     `json:"Manifest"`
	JudgedAt      time.Time    `json:"JudgedAt"`
}

type CaseResult struct {
	Index             int     `json:"Index"`
	GroupName         string  `json:"GroupName"`
	TestcaseKey       string  `json:"TestcaseKey"`
	Verdict           Verdict `json:"Verdict"`
	Score             int32   `json:"Score"`
	TimeMS            int     `json:"TimeMS"`
	MemoryKB          int     `json:"MemoryKB"`
	ExitCode          *int32  `json:"ExitCode"`
	Signal            string  `json:"Signal"`
	CheckerMessage    string  `json:"CheckerMessage"`
	OutputDiffSummary string  `json:"OutputDiffSummary"`
}

type Manifest struct {
	JudgeCoreVersion string         `json:"JudgeCoreVersion"`
	JudgeAgentID     string         `json:"JudgeAgentID"`
	LanguageRuntime  string         `json:"LanguageRuntime"`
	SandboxBackend   string         `json:"SandboxBackend"`
	SandboxProfile   string         `json:"SandboxProfile"`
	TestcaseSetHash  string         `json:"TestcaseSetHash"`
	CheckerHash      string         `json:"CheckerHash"`
	ValidatorHash    string         `json:"ValidatorHash"`
	TraceID          string         `json:"TraceID"`
	Raw              map[string]any `json:"Raw"`
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
