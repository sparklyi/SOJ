package submission

import (
	"context"

	"SOJ/internal/judge"
)

type submissionCompletionStore interface {
	GetSubmission(context.Context, int64) (SubmissionRecord, error)
	CompleteSubmissionWithResult(context.Context, int64, judge.Result, int32) (SubmissionRecord, error)
}

// SubmissionCompleter applies a terminal judge result exactly once.
type SubmissionCompleter struct {
	store submissionCompletionStore
}

func NewSubmissionCompleter(store submissionCompletionStore) *SubmissionCompleter {
	return &SubmissionCompleter{store: store}
}

func (s *SubmissionCompleter) CompleteSubmission(ctx context.Context, submissionID int64, result judge.Result) (SubmissionRecord, error) {
	updated, _, err := completeSubmission(ctx, s.store, submissionID, result)
	if err != nil {
		return SubmissionRecord{}, err
	}
	return updated, nil
}

func completeSubmission(ctx context.Context, store submissionCompletionStore, submissionID int64, result judge.Result) (SubmissionRecord, bool, error) {
	current, err := store.GetSubmission(ctx, submissionID)
	if err != nil {
		return SubmissionRecord{}, false, err
	}
	if terminalStatus(current.Status) {
		return current, false, nil
	}
	score := int32(0)
	if result.Verdict == judge.VerdictAccepted {
		score = 100
	}
	updated, err := store.CompleteSubmissionWithResult(ctx, submissionID, result, score)
	if err != nil {
		return SubmissionRecord{}, false, err
	}
	return updated, true, nil
}
