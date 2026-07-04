package submission

import (
	"context"
	"testing"
	"time"

	"SOJ/internal/judge"
)

func TestCompleteSubmissionRunsTerminalHookOnce(t *testing.T) {
	repo := newMemoryRepo()
	repo.submissions[1] = SubmissionRecord{
		ID:          1,
		UserID:      5,
		ProblemID:   11,
		ContestID:   int64Ptr(7),
		Status:      StatusRunning,
		SubmittedAt: time.Unix(100, 0).UTC(),
	}
	hook := &recordingTerminalHook{}
	service := NewService(ServiceOptions{Repository: repo, TerminalHook: hook})

	_, err := service.CompleteSubmission(context.Background(), 1, judge.Result{Verdict: judge.VerdictAccepted, JudgedAt: time.Unix(200, 0).UTC()})
	if err != nil {
		t.Fatalf("CompleteSubmission returned error: %v", err)
	}
	if hook.calls != 1 || hook.last.SubmissionID != 1 || hook.last.ContestID == nil || *hook.last.ContestID != 7 || hook.last.Status != StatusAccepted {
		t.Fatalf("hook calls=%d last=%+v", hook.calls, hook.last)
	}

	_, err = service.CompleteSubmission(context.Background(), 1, judge.Result{Verdict: judge.VerdictWrongAnswer})
	if err != nil {
		t.Fatalf("second CompleteSubmission returned error: %v", err)
	}
	if hook.calls != 1 {
		t.Fatalf("hook calls after terminal repeat = %d, want 1", hook.calls)
	}
}

type recordingTerminalHook struct {
	calls int
	last  TerminalSubmission
}

func (h *recordingTerminalHook) AfterSubmissionTerminal(ctx context.Context, submission TerminalSubmission) error {
	h.calls++
	h.last = submission
	return nil
}

func int64Ptr(value int64) *int64 { return &value }
