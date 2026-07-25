package submission

import (
	"context"

	"SOJ/internal/judge"
)

type judgeRunner interface {
	Judge(context.Context, judge.Request) (judge.Result, error)
}

type languageProvider interface {
	Languages(context.Context) ([]judge.Language, error)
}
