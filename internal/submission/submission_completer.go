package submission

import (
	"context"
	"time"

	"SOJ/internal/judge"
)

type submissionCompletionStore interface {
	GetSubmission(context.Context, int64) (SubmissionRecord, error)
	CompleteSubmissionWithResult(context.Context, int64, judge.Result, int32) (SubmissionRecord, error)
}

// SubmissionCompleter applies a terminal judge result exactly once.
type SubmissionCompleter struct {
	store        submissionCompletionStore
	terminalHook TerminalHook
	now          func() time.Time
}

func NewSubmissionCompleter(store submissionCompletionStore, terminalHook TerminalHook, now func() time.Time) *SubmissionCompleter {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SubmissionCompleter{store: store, terminalHook: terminalHook, now: now}
}

func (s *SubmissionCompleter) CompleteSubmission(ctx context.Context, submissionID int64, result judge.Result) (SubmissionRecord, error) {
	updated, completed, err := completeSubmission(ctx, s.store, submissionID, result)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if !completed || s.terminalHook == nil {
		return updated, nil
	}
	judgedAt := result.JudgedAt
	if judgedAt.IsZero() {
		judgedAt = s.now()
	}
	if err := s.terminalHook.AfterSubmissionTerminal(ctx, TerminalSubmission{
		SubmissionID: updated.ID,
		UserID:       updated.UserID,
		ProblemID:    updated.ProblemID,
		ContestID:    updated.ContestID,
		Status:       updated.Status,
		SubmittedAt:  updated.SubmittedAt,
		JudgedAt:     judgedAt,
	}); err != nil {
		return updated, err
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
