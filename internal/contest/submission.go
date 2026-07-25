package contest

import (
	"context"

	"SOJ/internal/submission"
)

func (s *Service) recordTerminalSubmission(ctx context.Context, terminal submission.TerminalSubmission) error {
	contestID := *terminal.ContestID
	contest, err := s.repo.GetContest(ctx, contestID)
	if err != nil {
		return err
	}
	problems, err := s.repo.ListContestProblems(ctx, contestID)
	if err != nil {
		return err
	}
	if !containsProblem(problems, terminal.ProblemID) {
		return nil
	}
	existing := ContestProblemResult{ContestID: contestID, UserID: terminal.UserID, ProblemID: terminal.ProblemID, Status: CellNone}
	results, err := s.repo.ListProblemResults(ctx, contestID)
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
	_, err = s.repo.UpsertProblemResult(ctx, existing)
	return err
}
