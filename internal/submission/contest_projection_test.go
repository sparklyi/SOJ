package submission

import (
	"testing"
	"time"
)

func TestBuildContestProblemProjectionUsesSubmissionOrder(t *testing.T) {
	start := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	projection := buildContestProblemProjection(start, []contestProjectionSubmission{
		{ID: 2, Status: StatusAccepted, SubmittedAt: start.Add(20 * time.Minute)},
		{ID: 1, Status: StatusWrongAnswer, SubmittedAt: start.Add(10 * time.Minute)},
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
	wantAcceptedAt := start.Add(20 * time.Minute)
	if projection.AcceptedAt == nil || !projection.AcceptedAt.Equal(wantAcceptedAt) {
		t.Fatalf("accepted at = %v, want %v", projection.AcceptedAt, wantAcceptedAt)
	}
}

func TestBuildContestProblemProjectionUsesCurrentRejudgeVerdict(t *testing.T) {
	start := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	projection := buildContestProblemProjection(start, []contestProjectionSubmission{
		{ID: 1, Status: StatusWrongAnswer, SubmittedAt: start.Add(10 * time.Minute)},
	})

	if projection.Status != "attempted" {
		t.Fatalf("status = %q, want attempted", projection.Status)
	}
	if projection.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", projection.Attempts)
	}
	if projection.AcceptedAt != nil || projection.PenaltyMinutes != 0 {
		t.Fatalf("rejudged wrong answer retained accepted projection: %+v", projection)
	}
	if projection.LastSubmissionID == nil || *projection.LastSubmissionID != 1 {
		t.Fatalf("last submission id = %v, want 1", projection.LastSubmissionID)
	}
}

func TestBuildContestProblemProjectionUsesEarliestCurrentAcceptance(t *testing.T) {
	start := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	projection := buildContestProblemProjection(start, []contestProjectionSubmission{
		{ID: 2, Status: StatusWrongAnswer, SubmittedAt: start.Add(20 * time.Minute)},
		{ID: 1, Status: StatusAccepted, SubmittedAt: start.Add(10 * time.Minute)},
	})

	if projection.Status != StatusAccepted || projection.Attempts != 1 || projection.PenaltyMinutes != 10 {
		t.Fatalf("projection = %+v, want first submission accepted with 10 minute penalty", projection)
	}
	if projection.LastSubmissionID == nil || *projection.LastSubmissionID != 1 {
		t.Fatalf("last submission id = %v, want 1 (first accepted)", projection.LastSubmissionID)
	}
}
