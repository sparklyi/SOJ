package contest

import (
	"context"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

type contestReaderStore interface {
	GetContest(context.Context, int64) (ContestRecord, error)
	ListContests(context.Context, ListContestFilter) ([]ContestRecord, int64, error)
	ListContestsByCursor(context.Context, ListContestFilter) ([]ContestRecord, error)
	ListContestProblems(context.Context, int64) ([]ContestProblem, error)
	GetRegistration(context.Context, int64, int64) (ContestRegistration, error)
	ListRegistrations(context.Context, int64) ([]ContestRegistration, error)
}

// ContestReader serves contest catalog and access-controlled read workflows.
type ContestReader struct {
	store contestReaderStore
	now   func() time.Time
}

// NewContestReader builds a contest reader with its read store and clock.
func NewContestReader(store contestReaderStore, now func() time.Time) *ContestReader {
	if store == nil {
		panic("contest reader store is required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ContestReader{store: store, now: now}
}

// GetContest returns a contest after applying viewer access and frontend fields.
func (r *ContestReader) GetContest(ctx context.Context, actor auth.Actor, id int64) (ContestRecord, error) {
	record, err := r.getContest(ctx, id)
	if err != nil {
		return ContestRecord{}, err
	}
	if err := r.canReadContest(ctx, actor, record); err != nil {
		return ContestRecord{}, err
	}
	return r.withFrontendContract(ctx, actor, record)
}

// ListContests returns a page of contests visible to the actor.
func (r *ContestReader) ListContests(ctx context.Context, actor auth.Actor, filter ListContestFilter) (ContestList, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	if actor.Admin() {
		filter.IncludePrivate = true
	} else if actor.Authenticated() {
		filter.VisibleToUserID = actor.UserID
	}
	filter.Limit = filter.PageSize
	filter.Offset = (filter.Page - 1) * filter.PageSize
	items, total, err := r.store.ListContests(ctx, filter)
	if err != nil {
		return ContestList{}, err
	}
	for i := range items {
		withProblems, err := r.withFrontendContract(ctx, actor, items[i])
		if err != nil {
			return ContestList{}, err
		}
		items[i] = withProblems
	}
	return ContestList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

// ListContestsByCursor returns a cursor page of contests visible to the actor.
func (r *ContestReader) ListContestsByCursor(ctx context.Context, actor auth.Actor, filter ListContestFilter) (ContestCursorPage, error) {
	if filter.PageSize <= 0 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	if actor.Admin() {
		filter.IncludePrivate = true
	} else if actor.Authenticated() {
		filter.VisibleToUserID = actor.UserID
	}
	limit := filter.PageSize
	cursor := ContestCursor{
		StartAt: time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC),
		ID:      1<<63 - 1,
	}
	if filter.Cursor != nil {
		if filter.Cursor.ID <= 0 || filter.Cursor.StartAt.IsZero() {
			return ContestCursorPage{}, apperror.BadRequest("invalid_cursor", "cursor is invalid")
		}
		cursor = ContestCursor{StartAt: filter.Cursor.StartAt.UTC(), ID: filter.Cursor.ID}
	}
	filter.Cursor = &cursor
	filter.Limit = limit + 1
	filter.Offset = 0
	items, err := r.store.ListContestsByCursor(ctx, filter)
	if err != nil {
		return ContestCursorPage{}, err
	}
	hasMore := len(items) > int(limit)
	if hasMore {
		items = items[:limit]
	}
	for i := range items {
		withProblems, err := r.withFrontendContract(ctx, actor, items[i])
		if err != nil {
			return ContestCursorPage{}, err
		}
		items[i] = withProblems
	}
	page := ContestCursorPage{Items: items}
	if hasMore {
		last := items[len(items)-1]
		page.NextCursor = &ContestCursor{StartAt: last.StartAt, ID: last.ID}
	}
	return page, nil
}

func (r *ContestReader) getContest(ctx context.Context, id int64) (ContestRecord, error) {
	return r.store.GetContest(ctx, id)
}

func (r *ContestReader) listContestProblems(ctx context.Context, contestID int64) ([]ContestProblem, error) {
	return r.store.ListContestProblems(ctx, contestID)
}

func (r *ContestReader) listRegistrations(ctx context.Context, contestID int64) ([]ContestRegistration, error) {
	return r.store.ListRegistrations(ctx, contestID)
}

func (r *ContestReader) getRegistration(ctx context.Context, contestID, userID int64) (ContestRegistration, error) {
	return r.store.GetRegistration(ctx, contestID, userID)
}

func (r *ContestReader) withFrontendContract(ctx context.Context, actor auth.Actor, record ContestRecord) (ContestRecord, error) {
	problems, err := r.listContestProblems(ctx, record.ID)
	if err != nil {
		return ContestRecord{}, err
	}
	record.Problems = problems
	record.ScoringMode = ScoringModeACM
	record.Registered = r.actorRegisteredForContest(ctx, actor, record.ID)
	return record, nil
}

func (r *ContestReader) actorRegisteredForContest(ctx context.Context, actor auth.Actor, contestID int64) bool {
	if !actor.Authenticated() {
		return false
	}
	registration, err := r.getRegistration(ctx, contestID, actor.UserID)
	return err == nil && registration.Status == RegistrationActive
}

func (r *ContestReader) canReadContest(ctx context.Context, actor auth.Actor, contest ContestRecord) error {
	if contest.Visibility == VisibilityPublic || actor.Admin() || actor.UserID == contest.OwnerUserID {
		return nil
	}
	if !actor.Authenticated() {
		return apperror.Unauthorized("auth_required", "authentication required")
	}
	registration, err := r.getRegistration(ctx, contest.ID, actor.UserID)
	if err == nil && registration.Status == RegistrationActive {
		return nil
	}
	return apperror.Forbidden("contest.not_allowed", "contest access denied")
}
