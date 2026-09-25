package judgecore

import (
	"context"
	"fmt"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/judgecore/checker"
	"SOJ/internal/judgecore/sandbox"
	"SOJ/internal/language"
)

const Version = "soj-judgecore-mvp"

type Core struct {
	sandbox        sandbox.Sandbox
	checker        checker.Checker
	now            func() time.Time
	cleanupTimeout time.Duration
}

type Options struct {
	Sandbox        sandbox.Sandbox
	Checker        checker.Checker
	Now            func() time.Time
	CleanupTimeout time.Duration
}

type Request struct {
	Language         language.Profile
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
	return &Core{sandbox: sandboxBackend, checker: outputChecker, now: now, cleanupTimeout: cleanupTimeout}
}

func (c *Core) Judge(ctx context.Context, request Request) (judge.Result, error) {
	if err := request.Validate(); err != nil {
		return judge.Result{}, err
	}
	profile := request.Language
	workspace, err := c.prepareWorkspace(ctx, profile, request.Source, limits(request.Timeout, request.MemoryKB, request.OutputLimitBytes))
	if err != nil {
		return judge.Result{}, err
	}
	defer c.cleanupWorkspace(workspace)

	compiled, err := c.sandbox.Compile(ctx, workspace, profile)
	if err != nil {
		return judge.Result{}, err
	}
	if compiled.Verdict != judge.VerdictAccepted {
		return c.compileFailed(profile, compiled), nil
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

// RunRequest is one execution of source with no expected output. It carries a
// resolved profile, like Request: the caller knows the slug and owns the
// catalog, the core knows how to execute and owns nothing else.
type RunRequest struct {
	Language language.Profile
	Source   []byte
	Stdin    string
	Timeout  time.Duration
	MemoryKB int64
}

// Validate reports whether the request is complete enough to execute.
func (r RunRequest) Validate() error {
	if r.Language.Slug == "" {
		return fmt.Errorf("language is required")
	}
	if len(r.Source) == 0 {
		return fmt.Errorf("source is required")
	}
	return nil
}

// Run compiles source and executes it once with the given stdin, returning the
// raw output. Nothing is compared and no testcase is involved: this is the
// operation behind a self-run / playground run.
func (c *Core) Run(ctx context.Context, request RunRequest) (judge.Result, error) {
	if err := request.Validate(); err != nil {
		return judge.Result{}, err
	}
	profile := request.Language
	workspace, err := c.prepareWorkspace(ctx, profile, request.Source, limits(request.Timeout, request.MemoryKB, 0))
	if err != nil {
		return judge.Result{}, err
	}
	defer c.cleanupWorkspace(workspace)

	compiled, err := c.sandbox.Compile(ctx, workspace, profile)
	if err != nil {
		return judge.Result{}, err
	}
	if compiled.Verdict != judge.VerdictAccepted {
		return c.compileFailed(profile, compiled), nil
	}

	run, err := c.sandbox.Run(ctx, workspace, profile, sandbox.RunRequest{
		Stdin:  request.Stdin,
		Limits: limits(request.Timeout, request.MemoryKB, 0),
	})
	if err != nil {
		return judge.Result{}, err
	}
	return c.result(profile, judge.Result{
		Verdict:      run.Verdict,
		TimeMS:       run.TimeMS,
		MemoryKB:     run.MemoryKB,
		Stdout:       run.Stdout,
		Stderr:       run.Stderr,
		ErrorMessage: run.ErrorMessage,
		JudgedAt:     c.now(),
	}), nil
}

// prepareWorkspace allocates the sandbox workspace. Judging and running share
// it so the prepare/cleanup pairing cannot drift between the two paths.
func (c *Core) prepareWorkspace(ctx context.Context, profile language.Profile, source []byte, runLimits sandbox.Limits) (sandbox.Workspace, error) {
	return c.sandbox.Prepare(ctx, sandbox.PrepareRequest{
		Profile: profile,
		Source:  source,
		Limits:  runLimits,
	})
}

func (c *Core) cleanupWorkspace(workspace sandbox.Workspace) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), c.cleanupTimeout)
	defer cancel()
	_ = c.sandbox.Cleanup(cleanupCtx, workspace)
}

// compileFailed is the final result whenever compilation does not succeed:
// there is nothing to run, so both paths return it verbatim.
func (c *Core) compileFailed(profile language.Profile, compiled sandbox.CompileResult) judge.Result {
	return c.result(profile, judge.Result{
		Verdict:       compiled.Verdict,
		CompileOutput: compiled.Output,
		ErrorMessage:  compiled.ErrorMessage,
		JudgedAt:      c.now(),
	})
}

func (c *Core) result(profile language.Profile, result judge.Result) judge.Result {
	result.Manifest.JudgeCoreVersion = Version
	result.Manifest.LanguageRuntime = profile.RuntimeLabel()
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
	if r.Language.Slug == "" {
		return fmt.Errorf("language is required")
	}
	if len(r.Source) == 0 {
		return fmt.Errorf("source is required")
	}
	if len(r.Cases) == 0 {
		return fmt.Errorf("cases are required")
	}
	return nil
}
