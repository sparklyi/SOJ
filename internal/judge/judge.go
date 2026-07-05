package judge

import (
	"context"
	"time"
)

type Verdict string

const (
	EngineSOJAgent       = "soj-agent"
	DefaultAgentEndpoint = "agent://local"
	AgentEndpointPrefix  = "agent://"
)

const (
	VerdictAccepted            Verdict = "accepted"
	VerdictWrongAnswer         Verdict = "wrong_answer"
	VerdictTimeLimitExceeded   Verdict = "time_limit_exceeded"
	VerdictMemoryLimitExceeded Verdict = "memory_limit_exceeded"
	VerdictRuntimeError        Verdict = "runtime_error"
	VerdictCompileError        Verdict = "compile_error"
	VerdictSystemError         Verdict = "system_error"
)

type Language struct {
	ID        int64
	Name      string
	Enabled   bool
	TimeLimit time.Duration
	MemoryKB  int64
}

type Testcase struct {
	InputKey          string
	ExpectedOutputKey string
	TimeLimit         time.Duration
	MemoryKB          int64
}

type Request struct {
	LanguageID int64
	Source     []byte
	Stdin      string
	Testcases  []Testcase
	Timeout    time.Duration
}

type Result struct {
	Verdict       Verdict
	TimeMS        int
	MemoryKB      int
	Stdout        string
	Stderr        string
	CompileOutput string
	ErrorMessage  string
	JudgedAt      time.Time
}

type JudgeEngine interface {
	Judge(ctx context.Context, request Request) (Result, error)
	Languages(ctx context.Context) ([]Language, error)
}
