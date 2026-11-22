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

func TestScoreboardTieAcrossPages(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{
		ID: 1, OwnerUserID: 10, Visibility: VisibilityPublic, Status: StatusPublished,
		StartAt: start, EndAt: start.Add(3 * time.Hour), FreezeAt: start.Add(2 * time.Hour),
	}
	repo.problems[1] = []ContestProblem{{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1}}
	// Two equally ranked contestants (same score, same penalty, same display
	// name; tie broken by user ID) and one lower-ranked contestant.
	repo.registrations[1] = []ContestRegistration{
		{ID: 1, ContestID: 1, UserID: 20, DisplayName: "alice", Status: RegistrationActive, AcceptedCount: 1, PenaltyMinutes: 10},
		{ID: 2, ContestID: 1, UserID: 21, DisplayName: "alice", Status: RegistrationActive, AcceptedCount: 1, PenaltyMinutes: 10},
		{ID: 3, ContestID: 1, UserID: 22, DisplayName: "cara", Status: RegistrationActive},
	}
	service := newContestService(repo, func() time.Time { return start.Add(time.Hour) })

	seen := make(map[int64]int)
	var ranks []int32
	cursor := ""
	for page := 0; ; page++ {
		if page > 3 {
			t.Fatal("pagination did not terminate after 3 pages")
		}
		query := ScoreboardQuery{View: ScoreboardViewLive, PageSize: 1, Cursor: cursor}
		resp, err := service.Scoreboard(context.Background(), auth.Anonymous("request"), 1, query)
		if err != nil {
			t.Fatalf("page %d returned error: %v", page+1, err)
		}
		if len(resp.Rows) != 1 {
			t.Fatalf("page %d rows = %+v, want exactly one row", page+1, resp.Rows)
		}
		row := resp.Rows[0]
		seen[row.UserID]++
		ranks = append(ranks, row.Rank)
		if resp.NextCursor == nil {
			break
		}
		cursor = *resp.NextCursor
	}

	// The tied pair spans the page boundary: each appears exactly once, in
	// tie-break (user ID) order, both with rank 1; the third user has rank 3.
	if len(seen) != 3 || seen[20] != 1 || seen[21] != 1 || seen[22] != 1 {
		t.Fatalf("users seen across pages = %+v, want 20,21,22 exactly once each", seen)
	}
	wantRanks := []int32{1, 1, 3}
	if len(ranks) != len(wantRanks) {
		t.Fatalf("ranks across pages = %v, want %v", ranks, wantRanks)
	}
	for i, want := range wantRanks {
		if ranks[i] != want {
			t.Fatalf("ranks across pages = %v, want %v (page %d rank = %d, want %d)", ranks, wantRanks, i+1, ranks[i], want)
		}
	}

	// Also verify page contents directly: page 1 = user 20 rank 1, page 2 =
	// user 21 rank 1 (the cross-page tie resume), page 3 = cara rank 3 with no
	// final cursor.
	first, err := service.Scoreboard(context.Background(), auth.Anonymous("request"), 1, ScoreboardQuery{View: ScoreboardViewLive, PageSize: 1})
	if err != nil || len(first.Rows) != 1 || first.Rows[0].UserID != 20 || first.Rows[0].Rank != 1 || first.NextCursor == nil {
		t.Fatalf("first page = %+v, err=%v, want user 20 rank 1 with next cursor", first, err)
	}
	second, err := service.Scoreboard(context.Background(), auth.Anonymous("request"), 1, ScoreboardQuery{View: ScoreboardViewLive, PageSize: 1, Cursor: *first.NextCursor})
	if err != nil || len(second.Rows) != 1 || second.Rows[0].UserID != 21 || second.Rows[0].Rank != 1 || second.NextCursor == nil {
		t.Fatalf("second page = %+v, err=%v, want user 21 rank 1 (tie resumed across the boundary) with next cursor", second, err)
	}
	third, err := service.Scoreboard(context.Background(), auth.Anonymous("request"), 1, ScoreboardQuery{View: ScoreboardViewLive, PageSize: 1, Cursor: *second.NextCursor})
	if err != nil || len(third.Rows) != 1 || third.Rows[0].UserID != 22 || third.Rows[0].DisplayName != "cara" || third.Rows[0].Rank != 3 {
		t.Fatalf("third page = %+v, err=%v, want cara rank 3", third, err)
	}
	if third.NextCursor != nil {
		t.Fatalf("final page next cursor = %q, want nil", *third.NextCursor)
	}
}

func TestScoreboardTieAcrossPagesCursorFields(t *testing.T) {
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.contests[1] = ContestRecord{
		ID: 1, OwnerUserID: 10, Visibility: VisibilityPublic, Status: StatusPublished,
		StartAt: start, EndAt: start.Add(3 * time.Hour), FreezeAt: start.Add(2 * time.Hour),
	}
	repo.problems[1] = []ContestProblem{{ContestID: 1, ProblemID: 101, Alias: "A", SortOrder: 1}}
	repo.registrations[1] = []ContestRegistration{
		{ID: 1, ContestID: 1, UserID: 20, DisplayName: "alice", Status: RegistrationActive, AcceptedCount: 1, PenaltyMinutes: 10},
		{ID: 2, ContestID: 1, UserID: 21, DisplayName: "alice", Status: RegistrationActive, AcceptedCount: 1, PenaltyMinutes: 10},
		{ID: 3, ContestID: 1, UserID: 22, DisplayName: "cara", Status: RegistrationActive},
	}
	service := newContestService(repo, func() time.Time { return start.Add(time.Hour) })

	first, err := service.Scoreboard(context.Background(), auth.Anonymous("request"), 1, ScoreboardQuery{View: ScoreboardViewLive, PageSize: 1})
	if err != nil || first.NextCursor == nil {
		t.Fatalf("first page = %+v, err=%v", first, err)
	}
	firstCursor, err := decodeScoreboardCursor(*first.NextCursor)
	if err != nil {
		t.Fatalf("decode first cursor: %v", err)
	}
	// The cursor must carry the tie key (score + penalty) plus the last rank so
	// the next page can resume the tie instead of restarting it.
	if firstCursor.ContestID != 1 || firstCursor.AfterAcceptedCount != 1 || firstCursor.AfterPenalty != 10 ||
		firstCursor.AfterDisplayName != "alice" || firstCursor.AfterUserID != 20 ||
		firstCursor.RowsSeen != 1 || firstCursor.LastRank != 1 {
		t.Fatalf("first cursor = %+v, want tie key (1,10,alice,20) with RowsSeen=1 LastRank=1", firstCursor)
	}

	second, err := service.Scoreboard(context.Background(), auth.Anonymous("request"), 1, ScoreboardQuery{View: ScoreboardViewLive, PageSize: 1, Cursor: *first.NextCursor})
	if err != nil || second.NextCursor == nil {
		t.Fatalf("second page = %+v, err=%v", second, err)
	}
	secondCursor, err := decodeScoreboardCursor(*second.NextCursor)
	if err != nil {
		t.Fatalf("decode second cursor: %v", err)
	}
	if secondCursor.AfterUserID != 21 || secondCursor.RowsSeen != 2 || secondCursor.LastRank != 1 {
		t.Fatalf("second cursor = %+v, want AfterUserID=21 RowsSeen=2 LastRank=1", secondCursor)
	}
}
