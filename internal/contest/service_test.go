package contest

import (
	"context"
	"errors"
	"testing"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

func TestScoreboardUsesACMPenaltyAndTieRank(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{
		ID:          1,
		OwnerUserID: 10,
		Title:       "ACM",
		Visibility:  VisibilityPublic,
		Status:      StatusPublished,
		StartAt:     start,
		EndAt:       start.Add(3 * time.Hour),
		FreezeAt:    start.Add(2 * time.Hour),
	}
	repo.problems[1] = []ContestProblem{
		{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1},
		{ContestID: 1, ProblemID: 102, Alias: "B", SortOrder: 2},
	}
	repo.registrations[1] = []ContestRegistration{
		{ID: 1, ContestID: 1, UserID: 20, DisplayName: "alice", Email: "alice@example.com", Status: RegistrationActive},
		{ID: 2, ContestID: 1, UserID: 21, DisplayName: "bob", Email: "bob@example.com", Status: RegistrationActive},
		{ID: 3, ContestID: 1, UserID: 22, DisplayName: "cara", Email: "cara@example.com", Status: RegistrationActive},
	}
	repo.results[1] = []ContestProblemResult{
		{ContestID: 1, UserID: 20, ProblemID: 101, Status: CellAccepted, Attempts: 2, AcceptedAt: testTimePtr(start.Add(10 * time.Minute)), PenaltyMinutes: 30},
		{ContestID: 1, UserID: 21, ProblemID: 101, Status: CellAccepted, Attempts: 1, AcceptedAt: testTimePtr(start.Add(30 * time.Minute)), PenaltyMinutes: 30},
		{ContestID: 1, UserID: 22, ProblemID: 101, Status: CellAccepted, Attempts: 1, AcceptedAt: testTimePtr(start.Add(35 * time.Minute)), PenaltyMinutes: 35},
		{ContestID: 1, UserID: 22, ProblemID: 102, Status: CellAttempted, Attempts: 2},
	}
	service := NewService(repo, WithNow(func() time.Time { return start.Add(90 * time.Minute) }))

	board, err := service.Scoreboard(context.Background(), auth.Actor{UserID: 30, Role: auth.RoleUser}, 1, ScoreboardViewLive)
	if err != nil {
		t.Fatalf("Scoreboard returned error: %v", err)
	}
	if len(board.Rows) != 3 {
		t.Fatalf("row count = %d, want 3", len(board.Rows))
	}
	if board.Rows[0].Rank != 1 || board.Rows[0].DisplayName != "alice" || board.Rows[0].PenaltyMinutes != 30 {
		t.Fatalf("row[0] = %+v", board.Rows[0])
	}
	if board.Rows[1].Rank != 1 || board.Rows[1].DisplayName != "bob" || board.Rows[1].PenaltyMinutes != 30 {
		t.Fatalf("row[1] = %+v", board.Rows[1])
	}
	if board.Rows[2].Rank != 3 || board.Rows[2].DisplayName != "cara" || board.Rows[2].Cells[1].Attempts != 2 {
		t.Fatalf("row[2] = %+v", board.Rows[2])
	}
}

func TestFrozenScoreboardHidesAttemptsAfterFreeze(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	freeze := start.Add(time.Hour)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{
		ID:          1,
		OwnerUserID: 10,
		Title:       "Frozen",
		Visibility:  VisibilityPublic,
		Status:      StatusPublished,
		StartAt:     start,
		EndAt:       start.Add(3 * time.Hour),
		FreezeAt:    freeze,
	}
	repo.problems[1] = []ContestProblem{
		{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1},
		{ContestID: 1, ProblemID: 102, Alias: "B", SortOrder: 2},
	}
	repo.registrations[1] = []ContestRegistration{
		{ID: 1, ContestID: 1, UserID: 20, DisplayName: "alice", Email: "alice@example.com", Status: RegistrationActive},
	}
	repo.submissions[1] = []ContestSubmissionResult{
		{ID: 1, ContestID: 1, UserID: 20, ProblemID: 101, Status: CellAccepted, SubmittedAt: start.Add(20 * time.Minute), JudgedAt: start.Add(21 * time.Minute)},
		{ID: 2, ContestID: 1, UserID: 20, ProblemID: 102, Status: CellAttempted, SubmittedAt: freeze.Add(5 * time.Minute), JudgedAt: freeze.Add(6 * time.Minute)},
		{ID: 3, ContestID: 1, UserID: 20, ProblemID: 102, Status: CellAccepted, SubmittedAt: freeze.Add(7 * time.Minute), JudgedAt: freeze.Add(8 * time.Minute)},
	}
	service := NewService(repo, WithNow(func() time.Time { return freeze.Add(30 * time.Minute) }))

	board, err := service.Scoreboard(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, 1, ScoreboardViewFrozen)
	if err != nil {
		t.Fatalf("Scoreboard returned error: %v", err)
	}
	row := board.Rows[0]
	if row.Cells[0].Status != CellAccepted || row.AcceptedCount != 1 {
		t.Fatalf("first cell/row = %+v", row)
	}
	if row.Cells[1].Status != CellFrozen || row.Cells[1].FrozenAttempts != 2 || row.Cells[1].AcceptedAt != nil {
		t.Fatalf("second cell = %+v, want hidden frozen attempts", row.Cells[1])
	}
}

func TestFinalScoreboardFallsBackToProblemResultsWhenSnapshotMissing(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{
		ID:          1,
		OwnerUserID: 10,
		Title:       "Final",
		Visibility:  VisibilityPublic,
		Status:      StatusEnded,
		StartAt:     start,
		EndAt:       start.Add(time.Hour),
		FreezeAt:    start.Add(30 * time.Minute),
	}
	repo.problems[1] = []ContestProblem{{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1}}
	repo.registrations[1] = []ContestRegistration{{ID: 1, ContestID: 1, UserID: 20, DisplayName: "alice", Email: "alice@example.com", Status: RegistrationActive}}
	repo.results[1] = []ContestProblemResult{{ContestID: 1, UserID: 20, ProblemID: 101, Status: CellAccepted, Attempts: 1, AcceptedAt: testTimePtr(start.Add(40 * time.Minute)), PenaltyMinutes: 40}}
	service := NewService(repo, WithNow(func() time.Time { return start.Add(2 * time.Hour) }))

	board, err := service.Scoreboard(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, 1, ScoreboardViewFinal)
	if err != nil {
		t.Fatalf("Scoreboard returned error: %v", err)
	}
	if board.View != ScoreboardViewFinal || board.Rows[0].AcceptedCount != 1 || board.Rows[0].PenaltyMinutes != 40 {
		t.Fatalf("board = %+v", board)
	}
}

func TestLiveScoreboardAfterFreezeRequiresOwnerOrAdmin(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{
		ID:          1,
		OwnerUserID: 10,
		Title:       "Private live",
		Visibility:  VisibilityPublic,
		Status:      StatusPublished,
		StartAt:     start,
		EndAt:       start.Add(3 * time.Hour),
		FreezeAt:    start.Add(time.Hour),
	}
	repo.problems[1] = []ContestProblem{{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1}}
	service := NewService(repo, WithNow(func() time.Time { return start.Add(90 * time.Minute) }))

	_, err := service.Scoreboard(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, 1, ScoreboardViewLive)
	if codeOf(err) != "contest.scoreboard_hidden" {
		t.Fatalf("user live error = %v, want contest.scoreboard_hidden", err)
	}
	if _, err := service.Scoreboard(context.Background(), auth.Actor{UserID: 10, Role: auth.RoleUser}, 1, ScoreboardViewLive); err != nil {
		t.Fatalf("owner live returned error: %v", err)
	}
	if _, err := service.Scoreboard(context.Background(), auth.Actor{UserID: 99, Role: auth.RoleAdmin}, 1, ScoreboardViewLive); err != nil {
		t.Fatalf("admin live returned error: %v", err)
	}
}

func TestListContestsAppliesVisibilityRules(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{ID: 1, OwnerUserID: 10, Title: "Public", Visibility: VisibilityPublic, Status: StatusPublished, StartAt: start, EndAt: start.Add(time.Hour), FreezeAt: start.Add(30 * time.Minute)}
	repo.contests[2] = ContestRecord{ID: 2, OwnerUserID: 10, Title: "Private", Visibility: VisibilityPrivate, Status: StatusPublished, StartAt: start, EndAt: start.Add(time.Hour), FreezeAt: start.Add(30 * time.Minute)}
	repo.contests[3] = ContestRecord{ID: 3, OwnerUserID: 30, Title: "Registered", Visibility: VisibilityPrivate, Status: StatusPublished, StartAt: start, EndAt: start.Add(time.Hour), FreezeAt: start.Add(30 * time.Minute)}
	repo.registrations[3] = []ContestRegistration{{ID: 1, ContestID: 3, UserID: 20, Status: RegistrationActive}}
	service := NewService(repo)

	anonymous, err := service.ListContests(context.Background(), auth.Anonymous("req"), ListContestFilter{})
	if err != nil {
		t.Fatalf("anonymous ListContests returned error: %v", err)
	}
	if len(anonymous.Items) != 1 || anonymous.Total != 1 || anonymous.Items[0].ID != 1 {
		t.Fatalf("anonymous list = %+v, want public contest only", anonymous)
	}

	registered, err := service.ListContests(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, ListContestFilter{})
	if err != nil {
		t.Fatalf("registered ListContests returned error: %v", err)
	}
	if len(registered.Items) != 2 || registered.Total != 2 {
		t.Fatalf("registered list = %+v, want public plus registered private", registered)
	}

	admin, err := service.ListContests(context.Background(), auth.Actor{UserID: 99, Role: auth.RoleAdmin}, ListContestFilter{})
	if err != nil {
		t.Fatalf("admin ListContests returned error: %v", err)
	}
	if len(admin.Items) != 3 || admin.Total != 3 {
		t.Fatalf("admin list = %+v, want all contests", admin)
	}
}

func TestContestCRUDRegistrationAndPermissions(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	service := NewService(repo, WithNow(func() time.Time { return start.Add(-time.Hour) }))
	owner := auth.Actor{UserID: 10, Role: auth.RoleUser}

	_, err := service.CreateContest(context.Background(), owner, ContestInput{
		Title:      "Bad aliases",
		Visibility: VisibilityPublic,
		Status:     StatusDraft,
		StartAt:    start,
		EndAt:      start.Add(time.Hour),
		FreezeAt:   start.Add(30 * time.Minute),
		Problems: []ContestProblemInput{
			{ProblemID: 101, Alias: "A"},
			{ProblemID: 102, Alias: "A"},
		},
	})
	if codeOf(err) != "contest.problem_alias_conflict" {
		t.Fatalf("duplicate alias error = %v", err)
	}

	created, err := service.CreateContest(context.Background(), owner, ContestInput{
		Title:      "Invited",
		Visibility: VisibilityPrivate,
		Status:     StatusPublished,
		StartAt:    start,
		EndAt:      start.Add(time.Hour),
		FreezeAt:   start.Add(30 * time.Minute),
		InviteCode: "secret",
		Problems: []ContestProblemInput{
			{ProblemID: 101, Alias: "A"},
			{ProblemID: 102, Alias: "B"},
		},
	})
	if err != nil {
		t.Fatalf("CreateContest returned error: %v", err)
	}
	if created.InviteCodeHash == "" || created.InviteCodeHash == "secret" {
		t.Fatalf("invite hash = %q, want non-empty hash", created.InviteCodeHash)
	}
	if repo.problems[created.ID][1].SortOrder != 2 {
		t.Fatalf("problems = %+v", repo.problems[created.ID])
	}

	_, err = service.UpdateContest(context.Background(), auth.Actor{UserID: 11, Role: auth.RoleUser}, created.ID, ContestUpdateInput{Title: stringPtr("Nope")})
	if codeOf(err) != "contest.not_allowed" {
		t.Fatalf("stranger update error = %v", err)
	}
	updated, err := service.UpdateContest(context.Background(), auth.Actor{UserID: 99, Role: auth.RoleAdmin}, created.ID, ContestUpdateInput{Title: stringPtr("Updated")})
	if err != nil {
		t.Fatalf("admin UpdateContest returned error: %v", err)
	}
	if updated.Title != "Updated" {
		t.Fatalf("updated title = %q", updated.Title)
	}

	_, err = service.Register(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, created.ID, RegistrationInput{
		DisplayName: "alice",
		Email:       "alice@example.com",
		InviteCode:  "wrong",
	})
	if codeOf(err) != "contest.invite_code_invalid" {
		t.Fatalf("wrong invite error = %v", err)
	}
	registration, err := service.Register(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, created.ID, RegistrationInput{
		DisplayName: "alice",
		Email:       "alice@example.com",
		InviteCode:  "secret",
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if registration.UserID != 20 || registration.Status != RegistrationActive {
		t.Fatalf("registration = %+v", registration)
	}
	_, err = service.Register(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, created.ID, RegistrationInput{
		DisplayName: "alice",
		Email:       "alice@example.com",
		InviteCode:  "secret",
	})
	if codeOf(err) != "contest.registration_exists" {
		t.Fatalf("duplicate registration error = %v", err)
	}
	if _, err := service.DeleteContest(context.Background(), owner, created.ID); err != nil {
		t.Fatalf("DeleteContest returned error: %v", err)
	}
	if repo.contests[created.ID].Status != StatusArchived {
		t.Fatalf("status = %s, want archived", repo.contests[created.ID].Status)
	}
}

func TestValidateSubmissionRequiresPublishedRegistrationAndWindow(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{ID: 1, OwnerUserID: 10, Visibility: VisibilityPrivate, Status: StatusPublished, StartAt: start, EndAt: start.Add(time.Hour), FreezeAt: start.Add(30 * time.Minute)}
	repo.problems[1] = []ContestProblem{{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1}}
	service := NewService(repo, WithNow(func() time.Time { return start.Add(10 * time.Minute) }))

	err := service.ValidateSubmission(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, 101, 1)
	if codeOf(err) != "contest.registration_required" {
		t.Fatalf("unregistered submit error = %v", err)
	}
	repo.registrations[1] = []ContestRegistration{{ID: 1, ContestID: 1, UserID: 20, DisplayName: "alice", Email: "alice@example.com", Status: RegistrationActive}}
	if err := service.ValidateSubmission(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, 101, 1); err != nil {
		t.Fatalf("registered submit returned error: %v", err)
	}
	err = service.ValidateSubmission(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, 102, 1)
	if codeOf(err) != "contest.problem_not_found" {
		t.Fatalf("outside problem error = %v", err)
	}

	service = NewService(repo, WithNow(func() time.Time { return start.Add(2 * time.Hour) }))
	err = service.ValidateSubmission(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, 101, 1)
	if codeOf(err) != "contest.ended" {
		t.Fatalf("ended submit error = %v", err)
	}
}

func codeOf(err error) string {
	if err == nil {
		return ""
	}
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return err.Error()
}

func testTimePtr(value time.Time) *time.Time { return &value }
func stringPtr(value string) *string         { return &value }
