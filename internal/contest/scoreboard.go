package contest

import (
	"context"
	"errors"
	"sort"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/submission"
)

type ScoreboardResponse struct {
	ContestID   int64            `json:"contest_id"`
	View        ScoreboardView   `json:"view"`
	GeneratedAt time.Time        `json:"generated_at"`
	Problems    []ContestProblem `json:"problems"`
	Rows        []ScoreboardRow  `json:"rows"`
	NextCursor  *string          `json:"next_cursor,omitempty"`
}

type ScoreboardRow struct {
	Rank           int32            `json:"rank"`
	UserID         int64            `json:"user_id"`
	DisplayName    string           `json:"display_name"`
	AcceptedCount  int32            `json:"accepted_count"`
	PenaltyMinutes int32            `json:"penalty_minutes"`
	Cells          []ScoreboardCell `json:"cells"`
}

type ScoreboardCell struct {
	ProblemID        int64      `json:"problem_id"`
	Alias            string     `json:"alias"`
	Status           string     `json:"status"`
	Attempts         int32      `json:"attempts"`
	FrozenAttempts   int32      `json:"frozen_attempts,omitempty"`
	AcceptedAt       *time.Time `json:"accepted_at,omitempty"`
	PenaltyMinutes   int32      `json:"penalty_minutes"`
	LastSubmissionID *int64     `json:"last_submission_id,omitempty"`
}

type scoreboardStore interface {
	ListProblemResults(context.Context, int64) ([]ContestProblemResult, error)
	ListScoreboardRegistrations(context.Context, int64, scoreboardRowsQuery) ([]ContestRegistration, error)
	ListProblemResultsForUsers(context.Context, int64, []int64) ([]ContestProblemResult, error)
	ListTerminalSubmissions(context.Context, int64) ([]ContestSubmissionResult, error)
	ListScoreSnapshotCandidates(context.Context, time.Time, int32) ([]ScoreSnapshotCandidate, error)
	CreateScoreSnapshot(context.Context, ScoreboardSnapshot) (ScoreboardSnapshot, error)
	LatestScoreSnapshot(context.Context, int64, ScoreboardView) (ScoreboardSnapshot, error)
	ScoreSnapshotPage(context.Context, int64, ScoreboardView, int32, int32) (scoreSnapshotPageResult, error)
}

// ScoreboardService owns scoreboard reads and snapshot generation.
type ScoreboardService struct {
	reader *ContestReader
	store  scoreboardStore
}

// NewScoreboardService builds scoreboard workflows from contest reads and score storage.
func NewScoreboardService(reader *ContestReader, store scoreboardStore) *ScoreboardService {
	if reader == nil {
		panic("scoreboard reader is required")
	}
	if store == nil {
		panic("scoreboard store is required")
	}
	return &ScoreboardService{reader: reader, store: store}
}

// Scoreboard returns the requested contest scoreboard view.
func (s *ScoreboardService) Scoreboard(ctx context.Context, actor auth.Actor, contestID int64, query ScoreboardQuery) (ScoreboardResponse, error) {
	contest, err := s.reader.getContest(ctx, contestID)
	if err != nil {
		return ScoreboardResponse{}, err
	}
	actor, err = s.reader.actorWithContestRoles(ctx, actor, contestID)
	if err != nil {
		return ScoreboardResponse{}, err
	}
	if err := s.reader.canReadContestAs(ctx, actor, contest); err != nil {
		return ScoreboardResponse{}, err
	}
	view := s.defaultScoreboardView(contest, query.View)
	if err := s.canViewScoreboard(actor, contest, view); err != nil {
		return ScoreboardResponse{}, err
	}
	pageSize, err := normalizeScoreboardPageSize(query.PageSize)
	if err != nil {
		return ScoreboardResponse{}, err
	}
	cursor, err := decodeScoreboardCursor(query.Cursor)
	if err != nil {
		return ScoreboardResponse{}, err
	}
	if err := validateScoreboardCursor(cursor, contestID, view); err != nil {
		return ScoreboardResponse{}, err
	}
	if view == ScoreboardViewFinal || view == ScoreboardViewFrozen {
		return s.scoreSnapshotPage(ctx, contest, view, pageSize, cursor)
	}
	return s.liveScoreboardPage(ctx, contest, view, pageSize, cursor)
}

func (s *ScoreboardService) liveScoreboardPage(ctx context.Context, contest ContestRecord, view ScoreboardView, pageSize int32, cursor scoreboardCursor) (ScoreboardResponse, error) {
	problems, err := s.reader.listContestProblems(ctx, contest.ID)
	if err != nil {
		return ScoreboardResponse{}, err
	}
	registrations, err := s.store.ListScoreboardRegistrations(ctx, contest.ID, scoreboardRowsQuery{
		PageSize:           pageSize + 1,
		HasCursor:          cursor.ContestID != 0,
		AfterAcceptedCount: cursor.AfterAcceptedCount,
		AfterPenalty:       cursor.AfterPenalty,
		AfterDisplayName:   cursor.AfterDisplayName,
		AfterUserID:        cursor.AfterUserID,
	})
	if err != nil {
		return ScoreboardResponse{}, err
	}
	hasMore := len(registrations) > int(pageSize)
	if hasMore {
		registrations = registrations[:pageSize]
	}
	rows := rowsForScoreboardRegistrations(problems, registrations)
	if err := s.populateRowsWithResults(ctx, contest.ID, rows); err != nil {
		return ScoreboardResponse{}, err
	}
	applyPageRanks(rows, cursor)
	response := ScoreboardResponse{ContestID: contest.ID, View: view, GeneratedAt: s.reader.now().UTC(), Problems: problems, Rows: rows}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		lastRegistration := registrations[len(registrations)-1]
		next, err := encodeScoreboardCursor(scoreboardCursor{
			ContestID:          contest.ID,
			View:               view,
			AfterAcceptedCount: last.AcceptedCount,
			AfterPenalty:       last.PenaltyMinutes,
			AfterDisplayName:   last.DisplayName,
			AfterUserID:        lastRegistration.UserID,
			RowsSeen:           cursor.RowsSeen + int32(len(rows)),
			LastRank:           last.Rank,
		})
		if err != nil {
			return ScoreboardResponse{}, err
		}
		response.NextCursor = &next
	}
	return response, nil
}

func (s *ScoreboardService) scoreSnapshotPage(ctx context.Context, contest ContestRecord, view ScoreboardView, pageSize int32, cursor scoreboardCursor) (ScoreboardResponse, error) {
	afterOrdinal := int32(0)
	if cursor.SnapshotID != 0 {
		afterOrdinal = cursor.AfterOrdinal
	}
	page, err := s.store.ScoreSnapshotPage(ctx, contest.ID, view, afterOrdinal, pageSize+1)
	if err != nil {
		var appErr *apperror.Error
		if errors.As(err, &appErr) && appErr.HTTPStatus == 404 {
			return ScoreboardResponse{}, apperror.New("contest.scoreboard_not_ready", "scoreboard snapshot is not ready", 503)
		}
		return ScoreboardResponse{}, err
	}
	if cursor.SnapshotID != 0 && cursor.SnapshotID != page.Snapshot.ID {
		return ScoreboardResponse{}, apperror.BadRequest("invalid_cursor", "scoreboard snapshot cursor is stale")
	}
	rows := page.Rows
	hasMore := len(rows) > int(pageSize)
	if hasMore {
		rows = rows[:pageSize]
	}
	response := ScoreboardResponse{ContestID: contest.ID, View: view, GeneratedAt: page.Snapshot.GeneratedAt.UTC(), Problems: page.Snapshot.Problems, Rows: rows}
	if hasMore && len(rows) > 0 {
		next, err := encodeScoreboardCursor(scoreboardCursor{ContestID: contest.ID, View: view, SnapshotID: page.Snapshot.ID, AfterOrdinal: int32(len(rows)) + afterOrdinal})
		if err != nil {
			return ScoreboardResponse{}, err
		}
		response.NextCursor = &next
	}
	return response, nil
}

func (s *ScoreboardService) populateRowsWithResults(ctx context.Context, contestID int64, rows []ScoreboardRow) error {
	userIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		userIDs = append(userIDs, row.UserID)
	}
	if len(userIDs) == 0 {
		return nil
	}
	results, err := s.store.ListProblemResultsForUsers(ctx, contestID, userIDs)
	if err != nil {
		return err
	}
	byUserProblem := make(map[int64]map[int64]ContestProblemResult, len(userIDs))
	for _, result := range results {
		if byUserProblem[result.UserID] == nil {
			byUserProblem[result.UserID] = make(map[int64]ContestProblemResult)
		}
		byUserProblem[result.UserID][result.ProblemID] = result
	}
	for i := range rows {
		for j := range rows[i].Cells {
			result, ok := byUserProblem[rows[i].UserID][rows[i].Cells[j].ProblemID]
			if !ok {
				continue
			}
			applyResultToCell(&rows[i].Cells[j], result)
		}
	}
	return nil
}

func applyResultToCell(cell *ScoreboardCell, result ContestProblemResult) {
	cell.Status = result.Status
	cell.Attempts = result.Attempts
	if result.Status == CellAccepted && cell.Attempts > 0 {
		cell.Attempts--
	}
	cell.AcceptedAt = result.AcceptedAt
	cell.PenaltyMinutes = result.PenaltyMinutes
	cell.LastSubmissionID = result.LastSubmissionID
}

func rowsForScoreboardRegistrations(problems []ContestProblem, registrations []ContestRegistration) []ScoreboardRow {
	rows := make([]ScoreboardRow, 0, len(registrations))
	for _, registration := range registrations {
		cells := make([]ScoreboardCell, 0, len(problems))
		for _, problem := range problems {
			cells = append(cells, ScoreboardCell{ProblemID: problem.ProblemID, Alias: problem.Alias, Status: CellNone})
		}
		rows = append(rows, ScoreboardRow{
			UserID:         registration.UserID,
			DisplayName:    registration.DisplayName,
			AcceptedCount:  registration.AcceptedCount,
			PenaltyMinutes: registration.PenaltyMinutes,
			Cells:          cells,
		})
	}
	return rows
}

func applyPageRanks(rows []ScoreboardRow, cursor scoreboardCursor) {
	for i := range rows {
		if i == 0 && cursor.ContestID != 0 && rows[i].AcceptedCount == cursor.AfterAcceptedCount && rows[i].PenaltyMinutes == cursor.AfterPenalty {
			rows[i].Rank = cursor.LastRank
			continue
		}
		if i > 0 && rows[i].AcceptedCount == rows[i-1].AcceptedCount && rows[i].PenaltyMinutes == rows[i-1].PenaltyMinutes {
			rows[i].Rank = rows[i-1].Rank
			continue
		}
		rows[i].Rank = cursor.RowsSeen + int32(i) + 1
	}
}

// GenerateDueScoreSnapshots builds missing frozen and final snapshots.
func (s *ScoreboardService) GenerateDueScoreSnapshots(ctx context.Context, limit int32) (ScoreSnapshotGenerationResult, error) {
	if limit <= 0 {
		limit = 100
	}
	candidates, err := s.store.ListScoreSnapshotCandidates(ctx, s.reader.now(), limit)
	if err != nil {
		return ScoreSnapshotGenerationResult{}, err
	}
	var result ScoreSnapshotGenerationResult
	for _, candidate := range candidates {
		if candidate.View != ScoreboardViewFrozen && candidate.View != ScoreboardViewFinal {
			continue
		}
		generatedAt := s.reader.now()
		board, sourceRevision, err := s.buildScoreboard(ctx, candidate.Contest, candidate.View, generatedAt)
		if err != nil {
			return result, err
		}
		created, err := s.store.CreateScoreSnapshot(ctx, ScoreboardSnapshot{
			ContestID:      candidate.Contest.ID,
			View:           candidate.View,
			Board:          board,
			Problems:       board.Problems,
			Rows:           board.Rows,
			SourceRevision: sourceRevision,
			GeneratedAt:    generatedAt,
		})
		if err != nil {
			return result, err
		}
		if !created.Created {
			continue
		}
		switch candidate.View {
		case ScoreboardViewFrozen:
			result.Frozen++
		case ScoreboardViewFinal:
			result.Final++
		}
	}
	return result, nil
}

func (s *ScoreboardService) buildScoreboard(ctx context.Context, contest ContestRecord, view ScoreboardView, generatedAt time.Time) (ScoreboardResponse, int64, error) {
	problems, err := s.reader.listContestProblems(ctx, contest.ID)
	if err != nil {
		return ScoreboardResponse{}, 0, err
	}
	registrations, err := s.reader.listRegistrations(ctx, contest.ID)
	if err != nil {
		return ScoreboardResponse{}, 0, err
	}
	sourceRevision := contest.ScoreRevision
	if view == ScoreboardViewFrozen {
		submissions, err := s.store.ListTerminalSubmissions(ctx, contest.ID)
		if err != nil {
			return ScoreboardResponse{}, 0, err
		}
		if len(submissions) > 0 {
			return buildBoardFromSubmissions(contest, view, problems, registrations, submissions, generatedAt), sourceRevision, nil
		}
	}
	results, err := s.store.ListProblemResults(ctx, contest.ID)
	if err != nil {
		return ScoreboardResponse{}, 0, err
	}
	return buildBoardFromResults(contest, view, problems, registrations, results, generatedAt), sourceRevision, nil
}

func (s *ScoreboardService) defaultScoreboardView(contest ContestRecord, requested ScoreboardView) ScoreboardView {
	if requested != "" {
		return requested
	}
	now := s.reader.now()
	if !now.Before(contest.EndAt) {
		return ScoreboardViewFinal
	}
	if !now.Before(contest.FreezeAt) {
		return ScoreboardViewFrozen
	}
	return ScoreboardViewLive
}

func (s *ScoreboardService) canViewScoreboard(actor auth.Actor, contest ContestRecord, view ScoreboardView) error {
	now := s.reader.now()
	switch view {
	case ScoreboardViewLive:
		if now.Before(contest.FreezeAt) || canViewContestResults(actor, contest) {
			return nil
		}
		return apperror.Forbidden("contest.scoreboard_hidden", "live scoreboard is hidden after freeze time")
	case ScoreboardViewFrozen:
		if now.Before(contest.FreezeAt) {
			return apperror.BadRequest("invalid_argument", "frozen view is not available before freeze time")
		}
		return nil
	case ScoreboardViewFinal:
		if now.Before(contest.EndAt) {
			return apperror.Forbidden("contest.scoreboard_hidden", "final scoreboard is hidden before contest end")
		}
		return nil
	default:
		return apperror.BadRequest("invalid_argument", "scoreboard view is invalid")
	}
}

func buildBoardFromResults(
	contest ContestRecord,
	view ScoreboardView,
	problems []ContestProblem,
	registrations []ContestRegistration,
	results []ContestProblemResult,
	now time.Time,
) ScoreboardResponse {
	resultByUserProblem := make(map[int64]map[int64]ContestProblemResult)
	for _, result := range results {
		if resultByUserProblem[result.UserID] == nil {
			resultByUserProblem[result.UserID] = make(map[int64]ContestProblemResult)
		}
		resultByUserProblem[result.UserID][result.ProblemID] = result
	}
	rows := rowsForRegistrations(problems, registrations)
	for i := range rows {
		for j := range rows[i].Cells {
			result, ok := resultByUserProblem[rows[i].UserID][rows[i].Cells[j].ProblemID]
			if !ok {
				continue
			}
			cell := &rows[i].Cells[j]
			cell.Status = result.Status
			cell.Attempts = result.Attempts
			if result.Status == CellAccepted && cell.Attempts > 0 {
				cell.Attempts--
			}
			cell.AcceptedAt = result.AcceptedAt
			cell.PenaltyMinutes = result.PenaltyMinutes
			cell.LastSubmissionID = result.LastSubmissionID
			if view == ScoreboardViewFrozen && result.AcceptedAt != nil && result.AcceptedAt.After(contest.FreezeAt) {
				cell.Status = CellFrozen
				cell.Attempts = 0
				cell.FrozenAttempts = result.Attempts
				cell.AcceptedAt = nil
				cell.PenaltyMinutes = 0
				cell.LastSubmissionID = nil
				continue
			}
			if result.Status == CellAccepted {
				rows[i].AcceptedCount++
				rows[i].PenaltyMinutes += result.PenaltyMinutes
			}
		}
	}
	rankRows(rows)
	return ScoreboardResponse{ContestID: contest.ID, View: view, GeneratedAt: now.UTC(), Problems: problems, Rows: rows}
}

func buildBoardFromSubmissions(
	contest ContestRecord,
	view ScoreboardView,
	problems []ContestProblem,
	registrations []ContestRegistration,
	submissions []ContestSubmissionResult,
	now time.Time,
) ScoreboardResponse {
	sort.Slice(submissions, func(i, j int) bool {
		if submissions[i].SubmittedAt.Equal(submissions[j].SubmittedAt) {
			return submissions[i].ID < submissions[j].ID
		}
		return submissions[i].SubmittedAt.Before(submissions[j].SubmittedAt)
	})
	states := make(map[int64]map[int64]*submissionCellState)
	for _, sub := range submissions {
		if states[sub.UserID] == nil {
			states[sub.UserID] = make(map[int64]*submissionCellState)
		}
		state := states[sub.UserID][sub.ProblemID]
		if state == nil {
			state = &submissionCellState{}
			states[sub.UserID][sub.ProblemID] = state
		}
		judgedAt := sub.JudgedAt
		if sub.FirstJudgedAt != nil {
			judgedAt = *sub.FirstJudgedAt
		}
		visible := sub.SubmittedAt.Before(contest.FreezeAt) && !judgedAt.After(contest.FreezeAt)
		if view != ScoreboardViewFrozen || visible {
			applyVisibleSubmission(contest, state, sub)
			continue
		}
		state.hiddenAttempts++
	}
	rows := rowsForRegistrations(problems, registrations)
	for i := range rows {
		for j := range rows[i].Cells {
			state := states[rows[i].UserID][rows[i].Cells[j].ProblemID]
			if state == nil {
				continue
			}
			cell := &rows[i].Cells[j]
			if state.acceptedAt != nil {
				cell.Status = CellAccepted
				cell.Attempts = state.wrongBeforeAccepted
				cell.AcceptedAt = state.acceptedAt
				cell.PenaltyMinutes = state.penaltyMinutes
				cell.LastSubmissionID = state.lastSubmissionID
				rows[i].AcceptedCount++
				rows[i].PenaltyMinutes += state.penaltyMinutes
				continue
			}
			if state.hiddenAttempts > 0 {
				cell.Status = CellFrozen
				cell.FrozenAttempts = state.hiddenAttempts
				continue
			}
			if state.wrongAttempts > 0 {
				cell.Status = CellAttempted
				cell.Attempts = state.wrongAttempts
			}
		}
	}
	rankRows(rows)
	return ScoreboardResponse{ContestID: contest.ID, View: view, GeneratedAt: now.UTC(), Problems: problems, Rows: rows}
}

type submissionCellState struct {
	wrongAttempts       int32
	wrongBeforeAccepted int32
	hiddenAttempts      int32
	acceptedAt          *time.Time
	penaltyMinutes      int32
	lastSubmissionID    *int64
}

func applyVisibleSubmission(contest ContestRecord, state *submissionCellState, sub ContestSubmissionResult) {
	if state.acceptedAt != nil {
		return
	}
	if sub.Status == submission.StatusAccepted || sub.Status == CellAccepted {
		acceptedAt := sub.SubmittedAt
		submissionID := sub.ID
		state.acceptedAt = &acceptedAt
		state.wrongBeforeAccepted = state.wrongAttempts
		state.penaltyMinutes = int32(sub.SubmittedAt.Sub(contest.StartAt).Minutes()) + state.wrongAttempts*20
		state.lastSubmissionID = &submissionID
		return
	}
	state.wrongAttempts++
}

func rowsForRegistrations(problems []ContestProblem, registrations []ContestRegistration) []ScoreboardRow {
	rows := make([]ScoreboardRow, 0, len(registrations))
	for _, registration := range registrations {
		if registration.Status != RegistrationActive {
			continue
		}
		cells := make([]ScoreboardCell, 0, len(problems))
		for _, problem := range problems {
			cells = append(cells, ScoreboardCell{ProblemID: problem.ProblemID, Alias: problem.Alias, Status: CellNone})
		}
		rows = append(rows, ScoreboardRow{UserID: registration.UserID, DisplayName: registration.DisplayName, Cells: cells})
	}
	return rows
}

func rankRows(rows []ScoreboardRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].AcceptedCount != rows[j].AcceptedCount {
			return rows[i].AcceptedCount > rows[j].AcceptedCount
		}
		if rows[i].PenaltyMinutes != rows[j].PenaltyMinutes {
			return rows[i].PenaltyMinutes < rows[j].PenaltyMinutes
		}
		return rows[i].DisplayName < rows[j].DisplayName
	})
	var previous *ScoreboardRow
	for i := range rows {
		if previous != nil && rows[i].AcceptedCount == previous.AcceptedCount && rows[i].PenaltyMinutes == previous.PenaltyMinutes {
			rows[i].Rank = previous.Rank
		} else {
			rows[i].Rank = int32(i + 1)
		}
		previous = &rows[i]
	}
}
