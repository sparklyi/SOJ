package problem

import (
	"context"
)

type Problem struct {
	ID                   int64
	Slug                 string
	Title                string
	Visibility           string
	OwnerUserID          int64
	CurrentStatementID   int64
	CurrentTestcaseSetID int64
}

type TestcaseSet struct {
	ID        int64
	ProblemID int64
	Version   int
	Cases     []Testcase
}

type Reader interface {
	GetForJudge(ctx context.Context, problemID int64) (Problem, error)
}

type TestcaseResolver interface {
	CurrentReadyTestcaseSet(ctx context.Context, problemID int64) (TestcaseSet, error)
}
