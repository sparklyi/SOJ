package submission

import (
	"context"

	"SOJ/internal/judge"
)

type judgeRunner interface {
	Judge(context.Context, judge.Request) (judge.Result, error)
}

// runExecutor is the only capability RunService needs: execute source with
// stdin and return the raw result. It deliberately does not ask for Judge -- a
// self-run has nothing to compare, and requiring a judging capability would
// force every run engine to also implement testcase comparison.
type runExecutor interface {
	Run(context.Context, judge.RunRequest) (judge.Result, error)
}
