package problem

import (
	"context"
	"time"
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
	Status    string
	Cases     []Testcase
}

type Testcase struct {
	ID               int64
	InputArtifactID  int64
	OutputArtifactID int64
	InputKey         string
	OutputKey        string
	TimeLimit        time.Duration
	MemoryKB         int64
}

type Reader interface {
	GetForJudge(ctx context.Context, problemID int64) (Problem, error)
}

type TestcaseResolver interface {
	CurrentReadyTestcaseSet(ctx context.Context, problemID int64) (TestcaseSet, error)
}
