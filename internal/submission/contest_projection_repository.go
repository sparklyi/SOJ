package submission

import (
	"context"
	"errors"
	"sort"
	"time"

	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type contestProblemProjectionLock struct {
	contestStart time.Time
	contestID    int64
	userID       int64
	enabled      bool
}

type contestProjectionSubmission struct {
	ID          int64
	Status      string
	SubmittedAt time.Time
	AttemptID   *int64
}

type contestProblemProjection struct {
	Status           string
	Attempts         int32
	AcceptedAt       *time.Time
	PenaltyMinutes   int32
	LastSubmissionID *int64
	BestSubmissionID *int64
	BestAttemptID    *int64
	LastAttemptID    *int64
}

func lockContestProblemProjection(ctx context.Context, q *db.Queries, submission SubmissionRecord) (contestProblemProjectionLock, error) {
	if submission.ContestID == nil {
		return contestProblemProjectionLock{}, nil
	}
	contest, err := q.GetContestByID(ctx, *submission.ContestID)
	if err != nil {
		return contestProblemProjectionLock{}, err
	}
	problems, err := q.ListContestProblems(ctx, *submission.ContestID)
	if err != nil {
		return contestProblemProjectionLock{}, err
	}
	for _, problem := range problems {
		if problem.ProblemID != submission.ProblemID {
			continue
		}
		_, err := q.LockContestRegistration(ctx, db.LockContestRegistrationParams{
			ContestID: *submission.ContestID,
			UserID:    submission.UserID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return contestProblemProjectionLock{}, nil
		}
		if err != nil {
			return contestProblemProjectionLock{}, err
		}
		params := db.EnsureContestProblemResultProjectionParams{
			ContestID: *submission.ContestID,
			UserID:    submission.UserID,
			ProblemID: submission.ProblemID,
		}
		if err := q.EnsureContestProblemResultProjection(ctx, params); err != nil {
			return contestProblemProjectionLock{}, err
		}
		if _, err := q.LockContestProblemResultProjection(ctx, db.LockContestProblemResultProjectionParams(params)); err != nil {
			return contestProblemProjectionLock{}, err
		}
		return contestProblemProjectionLock{
			contestStart: contest.StartAt.Time,
			contestID:    *submission.ContestID,
			userID:       submission.UserID,
			enabled:      true,
		}, nil
	}
	return contestProblemProjectionLock{}, nil
}

func rebuildContestProblemResult(ctx context.Context, q *db.Queries, submission SubmissionRecord, lock contestProblemProjectionLock) error {
	if !lock.enabled || submission.ContestID == nil {
		return nil
	}
	rows, err := q.ListContestProblemSubmissionsForProjection(ctx, db.ListContestProblemSubmissionsForProjectionParams{
		ContestID: pgtype.Int8{Int64: *submission.ContestID, Valid: true},
		UserID:    submission.UserID,
		ProblemID: submission.ProblemID,
	})
	if err != nil {
		return err
	}
	items := make([]contestProjectionSubmission, 0, len(rows))
	for _, row := range rows {
		items = append(items, contestProjectionSubmission{
			ID:          row.ID,
			Status:      row.Status,
			SubmittedAt: row.SubmittedAt.Time,
			AttemptID:   int8Value(row.AttemptID),
		})
	}
	projection := buildContestProblemProjection(lock.contestStart, items)
	acceptedAt := pgtype.Timestamptz{}
	if projection.AcceptedAt != nil {
		acceptedAt = timestamptz(*projection.AcceptedAt)
	}
	_, err = q.UpsertContestProblemResult(ctx, db.UpsertContestProblemResultParams{
		ContestID:        *submission.ContestID,
		UserID:           submission.UserID,
		ProblemID:        submission.ProblemID,
		Status:           projection.Status,
		Attempts:         projection.Attempts,
		AcceptedAt:       acceptedAt,
		PenaltyMinutes:   projection.PenaltyMinutes,
		LastSubmissionID: int8Ptr(projection.LastSubmissionID),
		BestSubmissionID: int8Ptr(projection.BestSubmissionID),
		BestAttemptID:    int8Ptr(projection.BestAttemptID),
		LastAttemptID:    int8Ptr(projection.LastAttemptID),
	})
	if err != nil {
		return err
	}
	results, err := q.ListContestProblemResultsForUsers(ctx, db.ListContestProblemResultsForUsersParams{
		ContestID: lock.contestID,
		UserIds:   []int64{lock.userID},
	})
	if err != nil {
		return err
	}
	acceptedCount := int32(0)
	penaltyMinutes := int32(0)
	for _, result := range results {
		if result.Status != StatusAccepted {
			continue
		}
		acceptedCount++
		penaltyMinutes += result.PenaltyMinutes
	}
	_, err = q.UpdateContestRegistrationScore(ctx, db.UpdateContestRegistrationScoreParams{
		AcceptedCount:  acceptedCount,
		PenaltyMinutes: penaltyMinutes,
		ContestID:      lock.contestID,
		UserID:         lock.userID,
	})
	if err != nil {
		return err
	}
	_, err = q.IncrementContestScoreRevision(ctx, lock.contestID)
	return err
}

func buildContestProblemProjection(contestStart time.Time, submissions []contestProjectionSubmission) contestProblemProjection {
	ordered := append([]contestProjectionSubmission(nil), submissions...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SubmittedAt.Equal(ordered[j].SubmittedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].SubmittedAt.Before(ordered[j].SubmittedAt)
	})

	projection := contestProblemProjection{Status: "none"}
	for _, submission := range ordered {
		projection.Status = "attempted"
		projection.Attempts++
		lastSubmissionID := submission.ID
		projection.LastSubmissionID = &lastSubmissionID
		projection.LastAttemptID = copyInt64Ptr(submission.AttemptID)
		if submission.Status != StatusAccepted {
			continue
		}
		acceptedAt := submission.SubmittedAt
		projection.Status = StatusAccepted
		projection.AcceptedAt = &acceptedAt
		projection.PenaltyMinutes = int32(acceptedAt.Sub(contestStart).Minutes()) + (projection.Attempts-1)*20
		projection.BestSubmissionID = &lastSubmissionID
		projection.BestAttemptID = copyInt64Ptr(submission.AttemptID)
		break
	}
	return projection
}
