package contest

import (
	"context"
	"strings"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

type contestAuthoringStore interface {
	GetContest(context.Context, int64) (ContestRecord, error)
	ArchiveContest(context.Context, int64) (ContestRecord, error)
	WithTx(context.Context, func(context.Context, contestTransaction) error) error
}

// ContestAuthoring owns contest lifecycle writes and their atomic problem updates.
type ContestAuthoring struct {
	store  contestAuthoringStore
	reader *ContestReader
}

// NewContestAuthoring builds the contest lifecycle writer.
func NewContestAuthoring(store contestAuthoringStore, reader *ContestReader) *ContestAuthoring {
	if store == nil {
		panic("contest authoring store is required")
	}
	if reader == nil {
		panic("contest authoring reader is required")
	}
	return &ContestAuthoring{store: store, reader: reader}
}

// CreateContest creates a contest and its problem configuration atomically.
func (a *ContestAuthoring) CreateContest(ctx context.Context, actor auth.Actor, input ContestInput) (ContestRecord, error) {
	if !actor.Authenticated() {
		return ContestRecord{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	if input.Status == "" {
		input.Status = StatusDraft
	}
	if err := validateContestInput(input); err != nil {
		return ContestRecord{}, err
	}
	problems, err := contestProblems(0, input.Problems)
	if err != nil {
		return ContestRecord{}, err
	}
	record := ContestRecord{
		OwnerUserID:    actor.UserID,
		Title:          strings.TrimSpace(input.Title),
		Description:    input.Description,
		Visibility:     input.Visibility,
		Status:         input.Status,
		StartAt:        input.StartAt.UTC(),
		EndAt:          input.EndAt.UTC(),
		FreezeAt:       input.FreezeAt.UTC(),
		InviteCodeHash: hashInviteCode(input.InviteCode),
	}

	var created ContestRecord
	err = a.store.WithTx(ctx, func(ctx context.Context, tx contestTransaction) error {
		var err error
		created, err = tx.CreateContest(ctx, record)
		if err != nil {
			return err
		}
		for i := range problems {
			problems[i].ContestID = created.ID
		}
		if err := tx.ReplaceContestProblems(ctx, created.ID, problems); err != nil {
			return err
		}
		created.Problems = problems
		return nil
	})
	created.ScoringMode = ScoringModeACM
	return created, err
}

// UpdateContest updates a contest and replaces its problem configuration atomically.
func (a *ContestAuthoring) UpdateContest(ctx context.Context, actor auth.Actor, id int64, input ContestUpdateInput) (ContestRecord, error) {
	current, err := a.store.GetContest(ctx, id)
	if err != nil {
		return ContestRecord{}, err
	}
	if err := requireContestWriter(actor, current); err != nil {
		return ContestRecord{}, err
	}
	if err := validateContestUpdate(current, input); err != nil {
		return ContestRecord{}, err
	}
	var updated ContestRecord
	err = a.store.WithTx(ctx, func(ctx context.Context, tx contestTransaction) error {
		var err error
		updated, err = tx.UpdateContest(ctx, id, input)
		if err != nil {
			return err
		}
		if input.Problems != nil {
			problems, err := contestProblems(id, *input.Problems)
			if err != nil {
				return err
			}
			if err := tx.ReplaceContestProblems(ctx, id, problems); err != nil {
				return err
			}
			updated.Problems = problems
			updated.ScoringMode = ScoringModeACM
			updated.Registered = a.reader.actorRegisteredForContest(ctx, actor, updated.ID)
			return nil
		}
		updated, err = a.reader.withFrontendContract(ctx, actor, updated)
		return err
	})
	return updated, err
}

// DeleteContest archives a contest after checking ownership.
func (a *ContestAuthoring) DeleteContest(ctx context.Context, actor auth.Actor, id int64) (ContestRecord, error) {
	current, err := a.store.GetContest(ctx, id)
	if err != nil {
		return ContestRecord{}, err
	}
	if err := requireContestWriter(actor, current); err != nil {
		return ContestRecord{}, err
	}
	return a.store.ArchiveContest(ctx, id)
}
