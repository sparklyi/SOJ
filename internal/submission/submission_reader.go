package submission

import (
	"context"
	"fmt"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

type submissionReaderStore interface {
	GetSubmission(context.Context, int64) (SubmissionRecord, error)
	ListSubmissions(context.Context, ListSubmissionsInput) ([]SubmissionRecord, int64, error)
	ListSubmissionsByCursor(context.Context, ListSubmissionsInput) ([]SubmissionRecord, error)
	ListSubmissionsByUserBefore(context.Context, int64, SubmissionCursor, int32) ([]SubmissionRecord, error)
	ListSubmissionSummaries(context.Context, []int64, bool) (map[int64]SubmissionListSummary, error)
	GetSubmissionResult(context.Context, int64) (SubmissionResultRecord, error)
	GetLatestJudgeAttemptBySubmissionID(context.Context, int64) (JudgeAttemptRecord, error)
	ListJudgeCaseResults(context.Context, int64) ([]JudgeCaseResultRecord, error)
}

// SubmissionReader owns submission queries and result visibility projection.
type SubmissionReader struct {
	store         submissionReaderStore
	contestPolicy ContestResultVisibilityPolicy
}

func NewSubmissionReader(store submissionReaderStore, contestPolicy ContestResultVisibilityPolicy) *SubmissionReader {
	return &SubmissionReader{store: store, contestPolicy: contestPolicy}
}

func (s *SubmissionReader) GetSubmission(ctx context.Context, actor auth.Actor, id int64) (SubmissionView, error) {
	record, err := s.store.GetSubmission(ctx, id)
	if err != nil {
		return SubmissionView{}, err
	}
	if !actor.Admin() && (!actor.Authenticated() || actor.UserID != record.UserID) {
		return SubmissionView{}, apperror.Forbidden("submission.not_allowed", "submission access denied")
	}
	return s.submissionView(ctx, actor, record)
}

func (s *SubmissionReader) ListSubmissions(ctx context.Context, actor auth.Actor, input ListSubmissionsInput) ([]SubmissionView, int64, error) {
	if !actor.Authenticated() {
		return nil, 0, apperror.Unauthorized("auth_required", "authentication required")
	}
	if !actor.Admin() {
		input.UserID = &actor.UserID
	}
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 50
	}
	if input.Offset < 0 {
		input.Offset = 0
	}
	records, total, err := s.store.ListSubmissions(ctx, input)
	if err != nil {
		return nil, 0, err
	}
	views, err := s.submissionListViews(ctx, actor, records)
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

func (s *SubmissionReader) ListSubmissionsByCursor(ctx context.Context, actor auth.Actor, input ListSubmissionsInput) (SubmissionCursorPage, error) {
	if !actor.Authenticated() {
		return SubmissionCursorPage{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	if !actor.Admin() {
		input.UserID = &actor.UserID
	}
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 20
	}
	limit := input.Limit
	cursor := SubmissionCursor{
		SubmittedAt: time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC),
		ID:          1<<63 - 1,
	}
	if input.Cursor != nil {
		if input.Cursor.ID <= 0 || input.Cursor.SubmittedAt.IsZero() {
			return SubmissionCursorPage{}, apperror.BadRequest("invalid_cursor", "cursor is invalid")
		}
		cursor = SubmissionCursor{SubmittedAt: input.Cursor.SubmittedAt.UTC(), ID: input.Cursor.ID}
	}
	input.Cursor = &cursor
	input.Limit++
	records, err := s.store.ListSubmissionsByCursor(ctx, input)
	if err != nil {
		return SubmissionCursorPage{}, err
	}
	hasMore := len(records) > int(limit)
	if hasMore {
		records = records[:limit]
	}
	views, err := s.submissionListViews(ctx, actor, records)
	if err != nil {
		return SubmissionCursorPage{}, err
	}
	page := SubmissionCursorPage{Items: views}
	if hasMore {
		last := records[len(records)-1]
		page.NextCursor = &SubmissionCursor{SubmittedAt: last.SubmittedAt, ID: last.ID}
	}
	return page, nil
}

func (s *SubmissionReader) ListOwnSubmissionsByCursor(ctx context.Context, actor auth.Actor, input ListOwnSubmissionsCursorInput) (SubmissionCursorPage, error) {
	if !actor.Authenticated() {
		return SubmissionCursorPage{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 20
	}
	cursor := SubmissionCursor{
		SubmittedAt: time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC),
		ID:          1<<63 - 1,
	}
	if input.Cursor != nil {
		if input.Cursor.ID <= 0 || input.Cursor.SubmittedAt.IsZero() {
			return SubmissionCursorPage{}, apperror.BadRequest("invalid_cursor", "invalid submission cursor")
		}
		cursor = SubmissionCursor{SubmittedAt: input.Cursor.SubmittedAt.UTC(), ID: input.Cursor.ID}
	}
	records, err := s.store.ListSubmissionsByUserBefore(ctx, actor.UserID, cursor, input.Limit+1)
	if err != nil {
		return SubmissionCursorPage{}, err
	}
	hasMore := len(records) > int(input.Limit)
	if hasMore {
		records = records[:input.Limit]
	}
	views, err := s.submissionListViews(ctx, actor, records)
	if err != nil {
		return SubmissionCursorPage{}, err
	}
	page := SubmissionCursorPage{Items: views}
	if hasMore {
		last := records[len(records)-1]
		page.NextCursor = &SubmissionCursor{SubmittedAt: last.SubmittedAt, ID: last.ID}
	}
	return page, nil
}

func (s *SubmissionReader) submissionListViews(ctx context.Context, actor auth.Actor, records []SubmissionRecord) ([]SubmissionView, error) {
	visibilities, err := s.submissionListVisibilities(ctx, actor, records)
	if err != nil {
		return nil, err
	}
	submissionIDs := make([]int64, 0, len(records))
	includeAttempts := false
	for _, record := range records {
		visibility := visibilities[record.ID]
		if visibility.ShowResult && terminalStatus(record.Status) {
			submissionIDs = append(submissionIDs, record.ID)
			includeAttempts = includeAttempts || visibility.ShowAdminDiagnostics
		}
	}
	summaries, err := s.store.ListSubmissionSummaries(ctx, submissionIDs, includeAttempts)
	if err != nil {
		return nil, err
	}
	views := make([]SubmissionView, 0, len(records))
	for _, record := range records {
		visibility := visibilities[record.ID]
		view := SubmissionView{Submission: record, Visibility: visibility.Visibility}
		if visibility.ShowResult && terminalStatus(record.Status) {
			if summary, ok := summaries[record.ID]; ok && summary.Result != nil {
				view.Result = summary.Result
				if visibility.ShowAdminDiagnostics && summary.LatestAttempt != nil {
					view.AdminDiagnostics = summary.LatestAttempt
				}
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *SubmissionReader) submissionView(ctx context.Context, actor auth.Actor, record SubmissionRecord) (SubmissionView, error) {
	visibility, err := s.submissionVisibility(ctx, actor, record)
	if err != nil {
		return SubmissionView{}, err
	}

	view := SubmissionView{Submission: record, Visibility: visibility.Visibility}
	if !visibility.ShowResult || !terminalStatus(record.Status) {
		return view, nil
	}
	result, err := s.store.GetSubmissionResult(ctx, record.ID)
	if err != nil {
		if appErr, ok := err.(*apperror.Error); ok && appErr.HTTPStatus == 404 {
			return view, nil
		}
		return SubmissionView{}, err
	}
	view.Result = &result

	attempt, err := s.store.GetLatestJudgeAttemptBySubmissionID(ctx, record.ID)
	if err != nil {
		if appErr, ok := err.(*apperror.Error); ok && appErr.HTTPStatus == 404 {
			return view, nil
		}
		return SubmissionView{}, err
	}
	if visibility.ShowAdminDiagnostics {
		view.AdminDiagnostics = &attempt
	}
	if visibility.ShowCases {
		cases, err := s.store.ListJudgeCaseResults(ctx, attempt.ID)
		if err != nil {
			return SubmissionView{}, err
		}
		view.Cases = cases
	}
	return view, nil
}

func (s *SubmissionReader) submissionListVisibilities(ctx context.Context, actor auth.Actor, records []SubmissionRecord) (map[int64]SubmissionResultVisibility, error) {
	visibilities := make(map[int64]SubmissionResultVisibility, len(records))
	contestSubmissions := make([]ContestSubmissionVisibility, 0, len(records))
	for _, record := range records {
		visibilities[record.ID] = SubmissionResultVisibility{ShowResult: true, ShowCases: true, ShowAdminDiagnostics: actor.Admin(), Visibility: "visible"}
		if record.ContestID == nil {
			continue
		}
		contestSubmissions = append(contestSubmissions, contestSubmissionVisibility(record))
	}
	if len(contestSubmissions) == 0 {
		return visibilities, nil
	}
	if policy, ok := s.contestPolicy.(ContestResultVisibilityBatchPolicy); ok {
		batchVisibilities, err := policy.SubmissionResultVisibilities(ctx, actor, contestSubmissions)
		if err != nil {
			return nil, err
		}
		for _, submission := range contestSubmissions {
			visibility, ok := batchVisibilities[submission.ID]
			if !ok {
				return nil, fmt.Errorf("contest visibility policy did not return submission %d", submission.ID)
			}
			if actor.Admin() {
				visibility.ShowAdminDiagnostics = true
			}
			visibilities[submission.ID] = visibility
		}
		return visibilities, nil
	}
	for _, record := range records {
		if record.ContestID == nil {
			continue
		}
		visibility, err := s.submissionVisibility(ctx, actor, record)
		if err != nil {
			return nil, err
		}
		visibilities[record.ID] = visibility
	}
	return visibilities, nil
}

func (s *SubmissionReader) submissionVisibility(ctx context.Context, actor auth.Actor, record SubmissionRecord) (SubmissionResultVisibility, error) {
	visibility := SubmissionResultVisibility{ShowResult: true, ShowCases: true, ShowAdminDiagnostics: actor.Admin(), Visibility: "visible"}
	if record.ContestID == nil {
		return visibility, nil
	}
	if s.contestPolicy != nil {
		policyVisibility, err := s.contestPolicy.SubmissionResultVisibility(ctx, actor, contestSubmissionVisibility(record))
		if err != nil {
			return SubmissionResultVisibility{}, err
		}
		if actor.Admin() {
			policyVisibility.ShowAdminDiagnostics = true
		}
		return policyVisibility, nil
	}
	if !actor.Admin() {
		visibility.ShowCases = false
	}
	return visibility, nil
}
