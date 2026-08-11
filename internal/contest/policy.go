package contest

import (
	"context"
	"strings"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/submission"
)

type contestRegistrationWriter interface {
	CreateRegistration(context.Context, ContestRegistration) (ContestRegistration, error)
}

// ContestPolicy owns registration, submission admission, and result visibility rules.
type ContestPolicy struct {
	reader        *ContestReader
	registrations contestRegistrationWriter
}

// NewContestPolicy builds contest policies with their registration writer.
func NewContestPolicy(reader *ContestReader, registrations contestRegistrationWriter) *ContestPolicy {
	if reader == nil {
		panic("contest policy reader is required")
	}
	if registrations == nil {
		panic("contest registration writer is required")
	}
	return &ContestPolicy{reader: reader, registrations: registrations}
}

// AuthorizeContestRejudge checks whether an actor may rejudge a contest.
func (p *ContestPolicy) AuthorizeContestRejudge(ctx context.Context, actor auth.Actor, id int64) error {
	contest, err := p.reader.getContest(ctx, id)
	if err != nil {
		return err
	}
	return requireContestWriter(actor, contest)
}

// ValidateContestRejudgeTarget checks whether a contest is ended and rejudgeable.
func (p *ContestPolicy) ValidateContestRejudgeTarget(ctx context.Context, id int64) error {
	contest, err := p.reader.getContest(ctx, id)
	if err != nil {
		return err
	}
	if contest.Status != StatusEnded {
		return apperror.Conflict("rejudge.contest_not_ended", "contest must be ended before rejudge")
	}
	return nil
}

// Register registers an authenticated actor for a contest.
func (p *ContestPolicy) Register(ctx context.Context, actor auth.Actor, contestID int64, input RegistrationInput) (ContestRegistration, error) {
	if !actor.Authenticated() {
		return ContestRegistration{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	contest, err := p.reader.getContest(ctx, contestID)
	if err != nil {
		return ContestRegistration{}, err
	}
	if contest.Visibility == VisibilityPrivate && contest.InviteCodeHash == "" {
		return ContestRegistration{}, apperror.Forbidden("contest.invite_code_required", "invite code is required")
	}
	if contest.Visibility == VisibilityPrivate && contest.InviteCodeHash != hashInviteCode(input.InviteCode) {
		return ContestRegistration{}, apperror.Forbidden("contest.invite_code_invalid", "invite code is invalid")
	}
	displayName := strings.TrimSpace(input.DisplayName)
	email := strings.TrimSpace(input.Email)
	if displayName == "" || email == "" {
		return ContestRegistration{}, apperror.BadRequest("request.invalid", "display_name and email are required")
	}
	return p.registrations.CreateRegistration(ctx, ContestRegistration{
		ContestID:   contestID,
		UserID:      actor.UserID,
		DisplayName: displayName,
		Email:       email,
		Status:      RegistrationActive,
	})
}

// ValidateSubmission checks contest admission for a submission.
func (p *ContestPolicy) ValidateSubmission(ctx context.Context, actor auth.Actor, problemID, contestID int64) error {
	if !actor.Authenticated() {
		return apperror.Unauthorized("auth_required", "authentication required")
	}
	contest, err := p.reader.getContest(ctx, contestID)
	if err != nil {
		return err
	}
	if contest.Status != StatusPublished && contest.Status != StatusRunning {
		return apperror.Forbidden("contest.not_started", "contest is not accepting submissions")
	}
	now := p.reader.now()
	if now.Before(contest.StartAt) {
		return apperror.Forbidden("contest.not_started", "contest has not started")
	}
	if !now.Before(contest.EndAt) {
		return apperror.Forbidden("contest.ended", "contest has ended")
	}
	problems, err := p.reader.listContestProblems(ctx, contestID)
	if err != nil {
		return err
	}
	if !containsProblem(problems, problemID) {
		return apperror.NotFound("contest.problem_not_found", "problem is not in contest")
	}
	if actor.Admin() || actor.UserID == contest.OwnerUserID {
		return nil
	}
	registration, err := p.reader.getRegistration(ctx, contestID, actor.UserID)
	if err != nil || registration.Status != RegistrationActive {
		return apperror.Forbidden("contest.registration_required", "contest registration required")
	}
	return nil
}

// SubmissionResultVisibility applies contest freeze visibility to one submission.
func (p *ContestPolicy) SubmissionResultVisibility(ctx context.Context, actor auth.Actor, sub submission.ContestSubmissionVisibility) (submission.SubmissionResultVisibility, error) {
	contest, err := p.reader.getContest(ctx, sub.ContestID)
	if err != nil {
		return submission.SubmissionResultVisibility{}, err
	}
	if err := p.reader.canReadContest(ctx, actor, contest); err != nil {
		return submission.SubmissionResultVisibility{}, err
	}
	return submissionResultVisibility(contest, actor, sub, p.reader.now()), nil
}

// SubmissionResultVisibilities applies contest freeze visibility to many submissions.
func (p *ContestPolicy) SubmissionResultVisibilities(ctx context.Context, actor auth.Actor, submissions []submission.ContestSubmissionVisibility) (map[int64]submission.SubmissionResultVisibility, error) {
	visibilities := make(map[int64]submission.SubmissionResultVisibility, len(submissions))
	contests := make(map[int64]ContestRecord)
	now := p.reader.now()
	for _, sub := range submissions {
		contest, ok := contests[sub.ContestID]
		if !ok {
			var err error
			contest, err = p.reader.getContest(ctx, sub.ContestID)
			if err != nil {
				return nil, err
			}
			if err := p.reader.canReadContest(ctx, actor, contest); err != nil {
				return nil, err
			}
			contests[sub.ContestID] = contest
		}
		visibilities[sub.ID] = submissionResultVisibility(contest, actor, sub, now)
	}
	return visibilities, nil
}

func submissionResultVisibility(contest ContestRecord, actor auth.Actor, sub submission.ContestSubmissionVisibility, now time.Time) submission.SubmissionResultVisibility {
	if actor.Admin() || actor.UserID == contest.OwnerUserID {
		return submission.SubmissionResultVisibility{ShowResult: true, ShowCases: true, ShowAdminDiagnostics: true, Visibility: "visible"}
	}
	visible := submissionVisibleInFrozenWindow(contest, sub, now)
	if !visible {
		return submission.SubmissionResultVisibility{Visibility: "frozen"}
	}
	return submission.SubmissionResultVisibility{ShowResult: true, ShowCases: true, Visibility: "visible"}
}

func submissionVisibleInFrozenWindow(contest ContestRecord, sub submission.ContestSubmissionVisibility, now time.Time) bool {
	if now.Before(contest.FreezeAt) || !now.Before(contest.EndAt) {
		return true
	}
	if sub.JudgedAt == nil {
		return sub.SubmittedAt.Before(contest.FreezeAt)
	}
	return sub.SubmittedAt.Before(contest.FreezeAt) && !sub.JudgedAt.After(contest.FreezeAt)
}
