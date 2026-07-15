package judgecore

import (
	"context"
	"fmt"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/judgecore/checker"
	"SOJ/internal/judgecore/language"
	"SOJ/internal/judgecore/sandbox"
)

const Version = "soj-judgecore-mvp"

type Core struct {
	languages      *language.Registry
	sandbox        sandbox.Sandbox
	checker        checker.Checker
	now            func() time.Time
	cleanupTimeout time.Duration
}

type Options struct {
	Languages      *language.Registry
	Sandbox        sandbox.Sandbox
	Checker        checker.Checker
	Now            func() time.Time
	CleanupTimeout time.Duration
}

type Request struct {
	LanguageID       int64
	Source           []byte
	Cases            []Case
	Timeout          time.Duration
	MemoryKB         int64
	OutputLimitBytes int64
	Policy           checker.Policy
}

type Case struct {
	Index          int
	Input          string
	ExpectedOutput string
	TimeLimit      time.Duration
	MemoryKB       int64
	Score          int32
}

func New(options Options) *Core {
	registry := options.Languages
	if registry == nil {
		registry = language.DefaultRegistry()
	}
	sandboxBackend := options.Sandbox
	if sandboxBackend == nil {
		sandboxBackend = sandbox.NewProcessSandbox()
	}
	outputChecker := options.Checker
	if outputChecker == nil {
		outputChecker = checker.Builtin{}
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	cleanupTimeout := options.CleanupTimeout
	if cleanupTimeout <= 0 {
		cleanupTimeout = sandbox.DefaultCleanupTimeout
	}
	return &Core{languages: registry, sandbox: sandboxBackend, checker: outputChecker, now: now, cleanupTimeout: cleanupTimeout}
}

func (c *Core) Judge(ctx context.Context, request Request) (judge.Result, error) {
	if err := request.Validate(); err != nil {
		return judge.Result{}, err
	}
	profile, err := c.languages.ResolveID(request.LanguageID)
	if err != nil {
		return judge.Result{}, err
	}
	workspace, err := c.sandbox.Prepare(ctx, sandbox.PrepareRequest{
		Profile: profile,
		Source:  request.Source,
		Limits:  limits(request.Timeout, request.MemoryKB, request.OutputLimitBytes),
	})
	if err != nil {
		return judge.Result{}, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), c.cleanupTimeout)
		defer cancel()
		_ = c.sandbox.Cleanup(cleanupCtx, workspace)
	}()

	compiled, err := c.sandbox.Compile(ctx, workspace, profile)
	if err != nil {
		return judge.Result{}, err
	}
	if compiled.Verdict != judge.VerdictAccepted {
		return c.result(profile, judge.Result{
			Verdict:       compiled.Verdict,
			CompileOutput: compiled.Output,
			ErrorMessage:  compiled.ErrorMessage,
			JudgedAt:      c.now(),
		}), nil
	}

	results := make([]judge.CaseResult, 0, len(request.Cases))
	verdict := judge.VerdictAccepted
	var maxTime int
	var maxMemory int
	for i, item := range request.Cases {
		index := item.Index
		if index == 0 {
			index = i + 1
		}
		run, err := c.sandbox.Run(ctx, workspace, profile, sandbox.RunRequest{
			Stdin:  item.Input,
			Limits: limits(caseTimeout(item.TimeLimit, request.Timeout), caseMemory(item.MemoryKB, request.MemoryKB), request.OutputLimitBytes),
		})
		if err != nil {
			return judge.Result{}, err
		}
		caseVerdict := run.Verdict
		message := run.ErrorMessage
		diff := ""
		if caseVerdict == judge.VerdictAccepted {
			checked, err := c.checker.Check(ctx, checker.Request{Expected: item.ExpectedOutput, Actual: run.Stdout, Policy: request.Policy})
			if err != nil {
				return judge.Result{}, err
			}
			if !checked.Accepted {
				caseVerdict = judge.VerdictWrongAnswer
				message = checked.Message
				diff = checked.DiffSummary
			}
		}
		if verdict == judge.VerdictAccepted && caseVerdict != judge.VerdictAccepted {
			verdict = caseVerdict
		}
		if run.TimeMS > maxTime {
			maxTime = run.TimeMS
		}
		if run.MemoryKB > maxMemory {
			maxMemory = run.MemoryKB
		}
		results = append(results, judge.CaseResult{
			Index:             index,
			Verdict:           caseVerdict,
			Score:             caseScore(caseVerdict, item.Score),
			TimeMS:            run.TimeMS,
			MemoryKB:          run.MemoryKB,
			ExitCode:          run.ExitCode,
			Signal:            run.Signal,
			CheckerMessage:    message,
			OutputDiffSummary: diff,
		})
	}
	return c.result(profile, judge.Result{Verdict: verdict, TimeMS: maxTime, MemoryKB: maxMemory, Cases: results, JudgedAt: c.now()}), nil
}

func (c *Core) result(profile language.Profile, result judge.Result) judge.Result {
	result.Manifest.JudgeCoreVersion = Version
	result.Manifest.LanguageRuntime = profile.Runtime
	result.Manifest.SandboxBackend = c.sandbox.Name()
	result.Manifest.SandboxProfile = c.sandbox.Profile()
	return result
}

func limits(timeout time.Duration, memoryKB int64, outputLimitBytes ...int64) sandbox.Limits {
	var outputLimit int64
	if len(outputLimitBytes) > 0 {
		outputLimit = outputLimitBytes[0]
	}
	return sandbox.Limits{TimeLimit: timeout, MemoryKB: memoryKB, OutputLimitBytes: outputLimit}
}

func caseTimeout(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	if fallback > 0 {
		return fallback
	}
	return time.Second
}

func caseMemory(value, fallback int64) int64 {
	if value > 0 {
		return value
	}
	return fallback
}

func caseScore(verdict judge.Verdict, score int32) int32 {
	if verdict != judge.VerdictAccepted {
		return 0
	}
	if score > 0 {
		return score
	}
	return 100
}

func (r Request) Validate() error {
	if r.LanguageID == 0 {
		return fmt.Errorf("language_id is required")
	}
	if len(r.Source) == 0 {
		return fmt.Errorf("source is required")
	}
	if len(r.Cases) == 0 {
		return fmt.Errorf("cases are required")
	}
	return nil
}
