package contest

import (
	"context"
	"sort"
	"time"

	"SOJ/internal/apperror"
)

type memoryRepository struct {
	nextID            int64
	contestReads      int
	registrationReads int
	contests          map[int64]ContestRecord
	problems          map[int64][]ContestProblem
	registrations     map[int64][]ContestRegistration
	results           map[int64][]ContestProblemResult
	submissions       map[int64][]ContestSubmissionResult
	snapshots         map[int64][]ScoreboardSnapshot
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		nextID:        100,
		contests:      make(map[int64]ContestRecord),
		problems:      make(map[int64][]ContestProblem),
		registrations: make(map[int64][]ContestRegistration),
		results:       make(map[int64][]ContestProblemResult),
		submissions:   make(map[int64][]ContestSubmissionResult),
		snapshots:     make(map[int64][]ScoreboardSnapshot),
	}
}

func (r *memoryRepository) id() int64 {
	r.nextID++
	return r.nextID
}

func (r *memoryRepository) WithTx(ctx context.Context, fn func(context.Context, Repository) error) error {
	return fn(ctx, r)
}

func (r *memoryRepository) CreateContest(ctx context.Context, input ContestRecord) (ContestRecord, error) {
	input.ID = r.id()
	input.CreatedAt = time.Now().UTC()
	input.UpdatedAt = input.CreatedAt
	r.contests[input.ID] = input
	return input, nil
}

func (r *memoryRepository) GetContest(ctx context.Context, id int64) (ContestRecord, error) {
	r.contestReads++
	row, ok := r.contests[id]
	if !ok {
		return ContestRecord{}, apperror.NotFound("contest.not_found", "contest not found")
	}
	return row, nil
}

func (r *memoryRepository) ListContests(ctx context.Context, filter ListContestFilter) ([]ContestRecord, int64, error) {
	var rows []ContestRecord
	for _, row := range r.contests {
		if filter.Status != "" && row.Status != filter.Status {
			continue
		}
		if filter.Visibility != "" && row.Visibility != filter.Visibility {
			continue
		}
		if !filter.IncludePrivate && row.Visibility != VisibilityPublic {
			if filter.VisibleToUserID <= 0 {
				continue
			}
			if row.OwnerUserID != filter.VisibleToUserID && !r.activeRegistration(row.ID, filter.VisibleToUserID) {
				continue
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	return rows, int64(len(rows)), nil
}

func (r *memoryRepository) ListContestsByCursor(ctx context.Context, filter ListContestFilter) ([]ContestRecord, error) {
	rows, _, err := r.ListContests(ctx, filter)
	if err != nil {
		return nil, err
	}
	cursor := filter.Cursor
	if cursor == nil {
		cursor = &ContestCursor{StartAt: time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC), ID: 1<<63 - 1}
	}
	filtered := rows[:0]
	for _, row := range rows {
		if row.StartAt.After(cursor.StartAt) || (row.StartAt.Equal(cursor.StartAt) && row.ID >= cursor.ID) {
			continue
		}
		filtered = append(filtered, row)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].StartAt.Equal(filtered[j].StartAt) {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].StartAt.After(filtered[j].StartAt)
	})
	if filter.Limit > 0 && len(filtered) > int(filter.Limit) {
		filtered = filtered[:filter.Limit]
	}
	return filtered, nil
}

func (r *memoryRepository) activeRegistration(contestID, userID int64) bool {
	for _, row := range r.registrations[contestID] {
		if row.UserID == userID && row.Status == RegistrationActive {
			return true
		}
	}
	return false
}

func (r *memoryRepository) UpdateContest(ctx context.Context, id int64, input ContestUpdateInput) (ContestRecord, error) {
	row, err := r.GetContest(ctx, id)
	if err != nil {
		return ContestRecord{}, err
	}
	if input.Title != nil {
		row.Title = *input.Title
	}
	if input.Description != nil {
		row.Description = input.Description
	}
	if input.Visibility != nil {
		row.Visibility = *input.Visibility
	}
	if input.Status != nil {
		row.Status = *input.Status
	}
	if input.StartAt != nil {
		row.StartAt = *input.StartAt
	}
	if input.EndAt != nil {
		row.EndAt = *input.EndAt
	}
	if input.FreezeAt != nil {
		row.FreezeAt = *input.FreezeAt
	}
	if input.InviteCode != nil {
		row.InviteCodeHash = hashInviteCode(*input.InviteCode)
	}
	r.contests[id] = row
	return row, nil
}

func (r *memoryRepository) ArchiveContest(ctx context.Context, id int64) (ContestRecord, error) {
	row, err := r.GetContest(ctx, id)
	if err != nil {
		return ContestRecord{}, err
	}
	row.Status = StatusArchived
	r.contests[id] = row
	return row, nil
}

func (r *memoryRepository) ReplaceContestProblems(ctx context.Context, contestID int64, problems []ContestProblem) error {
	r.problems[contestID] = append([]ContestProblem(nil), problems...)
	return nil
}

func (r *memoryRepository) ListContestProblems(ctx context.Context, contestID int64) ([]ContestProblem, error) {
	rows := append([]ContestProblem(nil), r.problems[contestID]...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].SortOrder < rows[j].SortOrder })
	return rows, nil
}

func (r *memoryRepository) CreateRegistration(ctx context.Context, input ContestRegistration) (ContestRegistration, error) {
	for _, row := range r.registrations[input.ContestID] {
		if row.UserID == input.UserID {
			return ContestRegistration{}, apperror.Conflict("contest.registration_exists", "contest registration already exists")
		}
	}
	input.ID = r.id()
	input.Status = RegistrationActive
	input.RegisteredAt = time.Now().UTC()
	r.registrations[input.ContestID] = append(r.registrations[input.ContestID], input)
	return input, nil
}

func (r *memoryRepository) GetRegistration(ctx context.Context, contestID, userID int64) (ContestRegistration, error) {
	r.registrationReads++
	for _, row := range r.registrations[contestID] {
		if row.UserID == userID {
			return row, nil
		}
	}
	return ContestRegistration{}, apperror.NotFound("contest.registration_not_found", "contest registration not found")
}

func (r *memoryRepository) ListRegistrations(ctx context.Context, contestID int64) ([]ContestRegistration, error) {
	return append([]ContestRegistration(nil), r.registrations[contestID]...), nil
}

func (r *memoryRepository) ListProblemResults(ctx context.Context, contestID int64) ([]ContestProblemResult, error) {
	return append([]ContestProblemResult(nil), r.results[contestID]...), nil
}

func (r *memoryRepository) ListTerminalSubmissions(ctx context.Context, contestID int64) ([]ContestSubmissionResult, error) {
	return append([]ContestSubmissionResult(nil), r.submissions[contestID]...), nil
}

func (r *memoryRepository) UpsertProblemResult(ctx context.Context, result ContestProblemResult) (ContestProblemResult, error) {
	rows := r.results[result.ContestID]
	for i, row := range rows {
		if row.UserID == result.UserID && row.ProblemID == result.ProblemID {
			rows[i] = result
			r.results[result.ContestID] = rows
			return result, nil
		}
	}
	r.results[result.ContestID] = append(rows, result)
	return result, nil
}

func (r *memoryRepository) ListScoreSnapshotCandidates(ctx context.Context, now time.Time, limit int32) ([]ScoreSnapshotCandidate, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []ScoreSnapshotCandidate
	for _, contest := range r.contests {
		if contest.Status != StatusPublished && contest.Status != StatusRunning && contest.Status != StatusEnded {
			continue
		}
		if !now.Before(contest.FreezeAt) && !r.hasSnapshot(contest.ID, ScoreboardViewFrozen) {
			rows = append(rows, ScoreSnapshotCandidate{Contest: contest, View: ScoreboardViewFrozen})
		}
		if int32(len(rows)) >= limit {
			break
		}
		if !now.Before(contest.EndAt) && !r.hasSnapshot(contest.ID, ScoreboardViewFinal) {
			rows = append(rows, ScoreSnapshotCandidate{Contest: contest, View: ScoreboardViewFinal})
		}
		if int32(len(rows)) >= limit {
			break
		}
	}
	return rows, nil
}

func (r *memoryRepository) CreateScoreSnapshot(ctx context.Context, snapshot ScoreboardSnapshot) (ScoreboardSnapshot, error) {
	snapshot.ID = r.id()
	if snapshot.ContestID == 0 {
		snapshot.ContestID = snapshot.Board.ContestID
	}
	if snapshot.View == "" {
		snapshot.View = snapshot.Board.View
	}
	if snapshot.GeneratedAt.IsZero() {
		snapshot.GeneratedAt = snapshot.Board.GeneratedAt
	}
	r.snapshots[snapshot.ContestID] = append(r.snapshots[snapshot.ContestID], snapshot)
	return snapshot, nil
}

func (r *memoryRepository) LatestScoreSnapshot(ctx context.Context, contestID int64, view ScoreboardView) (ScoreboardSnapshot, error) {
	rows := r.snapshots[contestID]
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].View == view {
			return rows[i], nil
		}
	}
	return ScoreboardSnapshot{}, apperror.NotFound("contest.score_snapshot_not_found", "contest score snapshot not found")
}

func (r *memoryRepository) hasSnapshot(contestID int64, view ScoreboardView) bool {
	_, err := r.LatestScoreSnapshot(context.Background(), contestID, view)
	return err == nil
}
