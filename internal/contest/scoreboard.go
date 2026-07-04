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

func (s *Service) Scoreboard(ctx context.Context, actor auth.Actor, contestID int64, requested ScoreboardView) (ScoreboardResponse, error) {
	contest, err := s.repo.GetContest(ctx, contestID)
	if err != nil {
		return ScoreboardResponse{}, err
	}
	if err := s.canReadContest(ctx, actor, contest); err != nil {
		return ScoreboardResponse{}, err
	}
	view := s.defaultScoreboardView(contest, requested)
	if err := s.canViewScoreboard(actor, contest, view); err != nil {
		return ScoreboardResponse{}, err
	}
	if view == ScoreboardViewFinal || view == ScoreboardViewFrozen {
		snapshot, err := s.repo.LatestScoreSnapshot(ctx, contestID, view)
		if err == nil {
			return snapshot.Board, nil
		}
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.HTTPStatus != 404 {
			return ScoreboardResponse{}, err
		}
	}
	problems, err := s.repo.ListContestProblems(ctx, contestID)
	if err != nil {
		return ScoreboardResponse{}, err
	}
	registrations, err := s.repo.ListRegistrations(ctx, contestID)
	if err != nil {
		return ScoreboardResponse{}, err
	}
	if view == ScoreboardViewFrozen {
		submissions, err := s.repo.ListTerminalSubmissions(ctx, contestID)
		if err != nil {
			return ScoreboardResponse{}, err
		}
		if len(submissions) > 0 {
			return buildBoardFromSubmissions(contest, view, problems, registrations, submissions, s.now()), nil
		}
	}
	results, err := s.repo.ListProblemResults(ctx, contestID)
	if err != nil {
		return ScoreboardResponse{}, err
	}
	return buildBoardFromResults(contest, view, problems, registrations, results, s.now()), nil
}

func (s *Service) defaultScoreboardView(contest ContestRecord, requested ScoreboardView) ScoreboardView {
	if requested != "" {
		return requested
	}
	now := s.now()
	if !now.Before(contest.EndAt) {
		return ScoreboardViewFinal
	}
	if !now.Before(contest.FreezeAt) {
		return ScoreboardViewFrozen
	}
	return ScoreboardViewLive
}

func (s *Service) canViewScoreboard(actor auth.Actor, contest ContestRecord, view ScoreboardView) error {
	now := s.now()
	switch view {
	case ScoreboardViewLive:
		if now.Before(contest.FreezeAt) || actor.Admin() || actor.UserID == contest.OwnerUserID {
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
		if submissions[i].JudgedAt.Equal(submissions[j].JudgedAt) {
			return submissions[i].ID < submissions[j].ID
		}
		return submissions[i].JudgedAt.Before(submissions[j].JudgedAt)
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
		visible := sub.SubmittedAt.Before(contest.FreezeAt) && !sub.JudgedAt.After(contest.FreezeAt)
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
