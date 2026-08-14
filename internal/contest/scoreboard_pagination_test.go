package contest

import (
	"context"
	"testing"
	"time"

	"SOJ/internal/auth"
)

func TestNormalizeScoreboardPageSizeRejectsZero(t *testing.T) {
	_, err := normalizeScoreboardPageSize(0)
	if code := codeOf(err); code != "invalid_argument" {
		t.Fatalf("zero page size error code = %q, want invalid_argument", code)
	}
}

func TestScoreboardPaginatesAndPreservesTieRanks(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{
		ID: 1, OwnerUserID: 10, Visibility: VisibilityPublic, Status: StatusPublished,
		StartAt: start, EndAt: start.Add(3 * time.Hour), FreezeAt: start.Add(2 * time.Hour),
	}
	repo.problems[1] = []ContestProblem{{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1}}
	repo.registrations[1] = []ContestRegistration{
		{ID: 1, ContestID: 1, UserID: 20, DisplayName: "alice", Status: RegistrationActive, AcceptedCount: 1, PenaltyMinutes: 10},
		{ID: 2, ContestID: 1, UserID: 21, DisplayName: "bob", Status: RegistrationActive, AcceptedCount: 1, PenaltyMinutes: 10},
		{ID: 3, ContestID: 1, UserID: 22, DisplayName: "cara", Status: RegistrationActive},
	}
	repo.results[1] = []ContestProblemResult{
		{ContestID: 1, UserID: 20, ProblemID: 101, Status: CellAccepted, Attempts: 1, PenaltyMinutes: 10},
		{ContestID: 1, UserID: 21, ProblemID: 101, Status: CellAccepted, Attempts: 1, PenaltyMinutes: 10},
		{ContestID: 1, UserID: 22, ProblemID: 101, Status: CellAttempted, Attempts: 1},
	}
	service := newContestService(repo, func() time.Time { return start.Add(time.Hour) })

	first, err := service.Scoreboard(context.Background(), auth.Anonymous("request"), 1, ScoreboardQuery{View: ScoreboardViewLive, PageSize: 2})
	if err != nil {
		t.Fatalf("first scoreboard page returned error: %v", err)
	}
	if len(first.Rows) != 2 || first.NextCursor == nil {
		t.Fatalf("first page = %+v, want two rows and next cursor", first)
	}
	if first.Rows[0].Rank != 1 || first.Rows[1].Rank != 1 {
		t.Fatalf("first page ranks = %d,%d, want 1,1", first.Rows[0].Rank, first.Rows[1].Rank)
	}

	second, err := service.Scoreboard(context.Background(), auth.Anonymous("request"), 1, ScoreboardQuery{View: ScoreboardViewLive, PageSize: 2, Cursor: *first.NextCursor})
	if err != nil {
		t.Fatalf("second scoreboard page returned error: %v", err)
	}
	if len(second.Rows) != 1 || second.Rows[0].DisplayName != "cara" || second.Rows[0].Rank != 3 {
		t.Fatalf("second page = %+v, want cara at rank 3", second)
	}
	if second.NextCursor != nil {
		t.Fatalf("second page next cursor = %q, want nil", *second.NextCursor)
	}
}

func TestScoreboardRejectsCursorFromAnotherContest(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	for id := int64(1); id <= 2; id++ {
		repo.contests[id] = ContestRecord{
			ID: id, OwnerUserID: 10, Visibility: VisibilityPublic, Status: StatusPublished,
			StartAt: start, EndAt: start.Add(3 * time.Hour), FreezeAt: start.Add(2 * time.Hour),
		}
		repo.problems[id] = []ContestProblem{{ContestID: id, ProblemID: 101, Alias: "A", SortOrder: 1}}
		repo.registrations[id] = []ContestRegistration{
			{ID: id * 10, ContestID: id, UserID: 20, DisplayName: "alice", Status: RegistrationActive},
			{ID: id*10 + 1, ContestID: id, UserID: 21, DisplayName: "bob", Status: RegistrationActive},
		}
	}
	service := newContestService(repo, func() time.Time { return start.Add(time.Hour) })
	page, err := service.Scoreboard(context.Background(), auth.Anonymous("request"), 1, ScoreboardQuery{View: ScoreboardViewLive, PageSize: 1})
	if err != nil {
		t.Fatalf("scoreboard page returned error: %v", err)
	}
	if page.NextCursor == nil {
		t.Fatal("scoreboard page did not return a cursor")
	}
	_, err = service.Scoreboard(context.Background(), auth.Anonymous("request"), 2, ScoreboardQuery{View: ScoreboardViewLive, PageSize: 1, Cursor: *page.NextCursor})
	if code := codeOf(err); code != "invalid_cursor" {
		t.Fatalf("cross-contest cursor error code = %q, want invalid_cursor", code)
	}
}

func TestSnapshotScoreboardRejectsCursorAfterNewSnapshot(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{
		ID: 1, OwnerUserID: 10, Visibility: VisibilityPublic, Status: StatusEnded,
		StartAt: start, EndAt: start.Add(2 * time.Hour), FreezeAt: start.Add(time.Hour),
		ScoreRevision: 2,
	}
	repo.problems[1] = []ContestProblem{{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1}}
	rows := []ScoreboardRow{
		{Rank: 1, UserID: 20, DisplayName: "alice", Cells: []ScoreboardCell{{ProblemID: 101, Alias: "A", Status: CellAccepted}}},
		{Rank: 2, UserID: 21, DisplayName: "bob", Cells: []ScoreboardCell{{ProblemID: 101, Alias: "A", Status: CellAttempted}}},
	}
	_, err := repo.CreateScoreSnapshot(context.Background(), ScoreboardSnapshot{
		ContestID: 1, View: ScoreboardViewFinal, Problems: repo.problems[1], Rows: rows,
		Board: ScoreboardResponse{ContestID: 1, View: ScoreboardViewFinal, Problems: repo.problems[1], Rows: rows},
	})
	if err != nil {
		t.Fatalf("create initial snapshot: %v", err)
	}
	service := newContestService(repo, func() time.Time { return start.Add(3 * time.Hour) })
	first, err := service.Scoreboard(context.Background(), auth.Anonymous("request"), 1, ScoreboardQuery{View: ScoreboardViewFinal, PageSize: 1})
	if err != nil || first.NextCursor == nil {
		t.Fatalf("first snapshot page = %+v, err=%v", first, err)
	}
	_, err = repo.CreateScoreSnapshot(context.Background(), ScoreboardSnapshot{
		ContestID: 1, View: ScoreboardViewFinal, Problems: repo.problems[1], Rows: rows,
		Board:          ScoreboardResponse{ContestID: 1, View: ScoreboardViewFinal, Problems: repo.problems[1], Rows: rows},
		SourceRevision: 1,
	})
	if err != nil {
		t.Fatalf("create replacement snapshot: %v", err)
	}
	_, err = service.Scoreboard(context.Background(), auth.Anonymous("request"), 1, ScoreboardQuery{View: ScoreboardViewFinal, PageSize: 1, Cursor: *first.NextCursor})
	if code := codeOf(err); code != "invalid_cursor" {
		t.Fatalf("stale snapshot cursor error code = %q, want invalid_cursor", code)
	}
}

func TestSnapshotScoreboardReportsNotReadyWithoutSnapshot(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{
		ID: 1, OwnerUserID: 10, Visibility: VisibilityPublic, Status: StatusEnded,
		StartAt: start, EndAt: start.Add(2 * time.Hour), FreezeAt: start.Add(time.Hour),
	}
	repo.problems[1] = []ContestProblem{{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1}}
	service := newContestService(repo, func() time.Time { return start.Add(3 * time.Hour) })
	_, err := service.Scoreboard(context.Background(), auth.Anonymous("request"), 1, ScoreboardQuery{View: ScoreboardViewFinal, PageSize: 1})
	if code := codeOf(err); code != "contest.scoreboard_not_ready" {
		t.Fatalf("missing snapshot error code = %q, want contest.scoreboard_not_ready", code)
	}
}

func TestFinalSnapshotWaitsForPendingSubmission(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{
		ID: 1, OwnerUserID: 10, Visibility: VisibilityPublic, Status: StatusEnded,
		StartAt: start, EndAt: start.Add(2 * time.Hour), FreezeAt: start.Add(time.Hour),
	}
	repo.problems[1] = []ContestProblem{{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1}}
	repo.registrations[1] = []ContestRegistration{{ID: 1, ContestID: 1, UserID: 20, DisplayName: "alice", Status: RegistrationActive}}
	repo.submissions[1] = []ContestSubmissionResult{{ID: 1, ContestID: 1, UserID: 20, ProblemID: 101, Status: "queued", SubmittedAt: start.Add(time.Minute)}}
	service := newContestService(repo, func() time.Time { return start.Add(3 * time.Hour) })
	created, err := service.GenerateDueScoreSnapshots(context.Background(), 10)
	if err != nil {
		t.Fatalf("GenerateDueScoreSnapshots returned error: %v", err)
	}
	if created.Final != 0 {
		t.Fatalf("created final snapshots = %d, want 0 while submission is pending", created.Final)
	}
}

func TestSnapshotCreationIsIdempotentForSourceRevision(t *testing.T) {
	repo := newMemoryRepository()
	snapshot := ScoreboardSnapshot{ContestID: 1, View: ScoreboardViewFinal, SourceRevision: 7}
	first, err := repo.CreateScoreSnapshot(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("first snapshot create returned error: %v", err)
	}
	second, err := repo.CreateScoreSnapshot(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("duplicate snapshot create returned error: %v", err)
	}
	if !first.Created || second.Created || first.ID != second.ID {
		t.Fatalf("first=%+v second=%+v, want one created row", first, second)
	}
}

func TestFinalSnapshotRefreshesWhenContestRevisionAdvances(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{
		ID: 1, OwnerUserID: 10, Visibility: VisibilityPublic, Status: StatusEnded,
		StartAt: start, EndAt: start.Add(2 * time.Hour), FreezeAt: start.Add(time.Hour),
		ScoreRevision: 5,
	}
	repo.problems[1] = []ContestProblem{{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1}}
	repo.registrations[1] = []ContestRegistration{
		{ID: 1, ContestID: 1, UserID: 20, DisplayName: "alice", Status: RegistrationActive},
		{ID: 2, ContestID: 1, UserID: 21, DisplayName: "bob", Status: RegistrationActive},
	}
	repo.results[1] = []ContestProblemResult{{ContestID: 1, UserID: 21, ProblemID: 101, Status: CellAccepted, Attempts: 1, PenaltyMinutes: 10}}
	_, err := repo.CreateScoreSnapshot(context.Background(), ScoreboardSnapshot{ContestID: 1, View: ScoreboardViewFinal, SourceRevision: 5})
	if err != nil {
		t.Fatalf("create baseline snapshot: %v", err)
	}
	if _, err := repo.UpsertProblemResult(context.Background(), repo.results[1][0]); err != nil {
		t.Fatalf("advance contest projection revision: %v", err)
	}
	service := newContestService(repo, func() time.Time { return start.Add(3 * time.Hour) })
	created, err := service.GenerateDueScoreSnapshots(context.Background(), 10)
	if err != nil {
		t.Fatalf("GenerateDueScoreSnapshots returned error: %v", err)
	}
	if created.Final != 1 {
		t.Fatalf("created final snapshots = %d, want 1 after revision advance", created.Final)
	}
}
