package contest

import (
	"testing"
	"time"

	"SOJ/internal/submission"
)

func TestBuildBoardFromSubmissionsUsesACMSubmissionOrder(t *testing.T) {
	start := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	contest := ContestRecord{
		ID:       1,
		StartAt:  start,
		FreezeAt: start.Add(time.Hour),
	}
	problems := []ContestProblem{{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1}}
	registrations := []ContestRegistration{{
		ID:          1,
		ContestID:   1,
		UserID:      20,
		DisplayName: "alice",
		Status:      RegistrationActive,
	}}

	tests := []struct {
		name        string
		submissions []ContestSubmissionResult
		wantPenalty int32
	}{
		{
			name: "submission order wins over judge completion order",
			submissions: []ContestSubmissionResult{
				{ID: 2, ContestID: 1, UserID: 20, ProblemID: 101, Status: submission.StatusAccepted, SubmittedAt: start.Add(20 * time.Minute), JudgedAt: start.Add(21 * time.Minute)},
				{ID: 1, ContestID: 1, UserID: 20, ProblemID: 101, Status: submission.StatusWrongAnswer, SubmittedAt: start.Add(10 * time.Minute), JudgedAt: start.Add(25 * time.Minute)},
			},
			wantPenalty: 40,
		},
		{
			name: "submission id breaks equal submitted time ties",
			submissions: []ContestSubmissionResult{
				{ID: 2, ContestID: 1, UserID: 20, ProblemID: 101, Status: submission.StatusAccepted, SubmittedAt: start.Add(20 * time.Minute), JudgedAt: start.Add(21 * time.Minute)},
				{ID: 1, ContestID: 1, UserID: 20, ProblemID: 101, Status: submission.StatusWrongAnswer, SubmittedAt: start.Add(20 * time.Minute), JudgedAt: start.Add(25 * time.Minute)},
			},
			wantPenalty: 40,
		},
		{
			name: "output limit counts as a wrong attempt",
			submissions: []ContestSubmissionResult{
				{ID: 1, ContestID: 1, UserID: 20, ProblemID: 101, Status: submission.StatusOutputLimit, SubmittedAt: start.Add(10 * time.Minute), JudgedAt: start.Add(11 * time.Minute)},
				{ID: 2, ContestID: 1, UserID: 20, ProblemID: 101, Status: submission.StatusAccepted, SubmittedAt: start.Add(20 * time.Minute), JudgedAt: start.Add(21 * time.Minute)},
			},
			wantPenalty: 40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			board := buildBoardFromSubmissions(contest, ScoreboardViewFrozen, problems, registrations, tt.submissions, contest.FreezeAt)
			cell := board.Rows[0].Cells[0]
			if cell.Status != CellAccepted || cell.Attempts != 1 || cell.PenaltyMinutes != tt.wantPenalty {
				t.Fatalf("cell = %+v, want accepted with 1 wrong attempt and %d penalty minutes", cell, tt.wantPenalty)
			}
			if cell.LastSubmissionID == nil || *cell.LastSubmissionID != 2 {
				t.Fatalf("last submission id = %v, want 2", cell.LastSubmissionID)
			}
		})
	}
}
