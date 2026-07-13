package submission

import (
	"testing"
	"time"
)

func TestBuildContestProblemProjectionUsesSubmissionOrder(t *testing.T) {
	start := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	projection := buildContestProblemProjection(start, []contestProjectionSubmission{
		{ID: 2, Status: StatusAccepted, SubmittedAt: start.Add(20 * time.Minute), AttemptID: int64Ptr(22)},
		{ID: 1, Status: StatusWrongAnswer, SubmittedAt: start.Add(10 * time.Minute), AttemptID: int64Ptr(11)},
	})

	if projection.Status != StatusAccepted {
		t.Fatalf("status = %q, want %q", projection.Status, StatusAccepted)
	}
	if projection.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", projection.Attempts)
	}
	if projection.PenaltyMinutes != 40 {
		t.Fatalf("penalty minutes = %d, want 40", projection.PenaltyMinutes)
	}
	if projection.LastSubmissionID == nil || *projection.LastSubmissionID != 2 {
		t.Fatalf("last submission id = %v, want 2", projection.LastSubmissionID)
	}
	if projection.BestSubmissionID == nil || *projection.BestSubmissionID != 2 {
		t.Fatalf("best submission id = %v, want 2", projection.BestSubmissionID)
	}
	if projection.LastAttemptID == nil || *projection.LastAttemptID != 22 {
		t.Fatalf("last attempt id = %v, want 22", projection.LastAttemptID)
	}
}

func TestBuildContestProblemProjectionUsesCurrentRejudgeVerdict(t *testing.T) {
	start := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	projection := buildContestProblemProjection(start, []contestProjectionSubmission{
		{ID: 1, Status: StatusWrongAnswer, SubmittedAt: start.Add(10 * time.Minute), AttemptID: int64Ptr(102)},
	})

	if projection.Status != "attempted" {
		t.Fatalf("status = %q, want attempted", projection.Status)
	}
	if projection.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", projection.Attempts)
	}
	if projection.AcceptedAt != nil || projection.BestSubmissionID != nil || projection.BestAttemptID != nil {
		t.Fatalf("rejudged wrong answer retained accepted projection: %+v", projection)
	}
	if projection.LastAttemptID == nil || *projection.LastAttemptID != 102 {
		t.Fatalf("last attempt id = %v, want 102", projection.LastAttemptID)
	}
}

func TestBuildContestProblemProjectionUsesEarliestCurrentAcceptance(t *testing.T) {
	start := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	projection := buildContestProblemProjection(start, []contestProjectionSubmission{
		{ID: 2, Status: StatusWrongAnswer, SubmittedAt: start.Add(20 * time.Minute), AttemptID: int64Ptr(22)},
		{ID: 1, Status: StatusAccepted, SubmittedAt: start.Add(10 * time.Minute), AttemptID: int64Ptr(101)},
	})

	if projection.Status != StatusAccepted || projection.Attempts != 1 || projection.PenaltyMinutes != 10 {
		t.Fatalf("projection = %+v, want first submission accepted with 10 minute penalty", projection)
	}
	if projection.BestSubmissionID == nil || *projection.BestSubmissionID != 1 {
		t.Fatalf("best submission id = %v, want 1", projection.BestSubmissionID)
	}
	if projection.BestAttemptID == nil || *projection.BestAttemptID != 101 {
		t.Fatalf("best attempt id = %v, want rejudge attempt 101", projection.BestAttemptID)
	}
}
