package contest

import (
	"context"

	"SOJ/internal/submission"
)

type scoreboardProjectionStore interface {
	ListProblemResults(context.Context, int64) ([]ContestProblemResult, error)
	UpsertProblemResult(context.Context, ContestProblemResult) (ContestProblemResult, error)
}

// ScoreboardProjection updates the contest result projection after terminal submissions.
type ScoreboardProjection struct {
	reader *ContestReader
	store  scoreboardProjectionStore
}

// NewScoreboardProjection builds the terminal result projection workflow.
func NewScoreboardProjection(reader *ContestReader, store scoreboardProjectionStore) *ScoreboardProjection {
	if reader == nil {
		panic("scoreboard projection reader is required")
	}
	if store == nil {
		panic("scoreboard projection store is required")
	}
	return &ScoreboardProjection{reader: reader, store: store}
}

// AfterSubmissionTerminal updates the contest result projection for a terminal submission.
func (s *ScoreboardProjection) AfterSubmissionTerminal(ctx context.Context, terminal submission.TerminalSubmission) error {
	if terminal.ContestID == nil {
		return nil
	}
	return s.recordTerminalSubmission(ctx, terminal)
}

func (s *ScoreboardProjection) recordTerminalSubmission(ctx context.Context, terminal submission.TerminalSubmission) error {
	contestID := *terminal.ContestID
	contest, err := s.reader.getContest(ctx, contestID)
	if err != nil {
		return err
	}
	problems, err := s.reader.listContestProblems(ctx, contestID)
	if err != nil {
		return err
	}
	if !containsProblem(problems, terminal.ProblemID) {
		return nil
	}
	existing := ContestProblemResult{ContestID: contestID, UserID: terminal.UserID, ProblemID: terminal.ProblemID, Status: CellNone}
	results, err := s.store.ListProblemResults(ctx, contestID)
	if err != nil {
		return err
	}
	for _, result := range results {
		if result.UserID == terminal.UserID && result.ProblemID == terminal.ProblemID {
			existing = result
			break
		}
	}
	if existing.LastSubmissionID != nil && *existing.LastSubmissionID == terminal.SubmissionID {
		return nil
	}
	if existing.Status == CellAccepted {
		return nil
	}
	submissionID := terminal.SubmissionID
	existing.Attempts++
	existing.LastSubmissionID = &submissionID
	existing.UpdatedAt = terminal.JudgedAt
	if terminal.Status == submission.StatusAccepted {
		acceptedAt := terminal.SubmittedAt
		existing.Status = CellAccepted
		existing.AcceptedAt = &acceptedAt
		existing.PenaltyMinutes = int32(terminal.SubmittedAt.Sub(contest.StartAt).Minutes()) + (existing.Attempts-1)*20
	} else {
		existing.Status = CellAttempted
	}
	_, err = s.store.UpsertProblemResult(ctx, existing)
	return err
}
