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
	ListSubmissionSummaries(context.Context, []int64) (map[int64]SubmissionListSummary, error)
	ListJudgeCaseResults(context.Context, int64) ([]JudgeCaseResultRecord, error)
	GetArtifact(context.Context, int64) (ArtifactRecord, error)
}

// SubmissionReader owns submission queries and result visibility projection.
type SubmissionReader struct {
	store         submissionReaderStore
	contestPolicy ContestResultVisibilityPolicy
	sources       sourceReader
}

// NewSubmissionReader builds a reader with its query store, contest visibility
// policy, and the object source store it reads submission source from. A nil
// source store simply makes GetSubmissionSource return unavailable rather than
// preventing the reader from serving everything else.
func NewSubmissionReader(store submissionReaderStore, contestPolicy ContestResultVisibilityPolicy, sources sourceReader) *SubmissionReader {
	if store == nil {
		panic("submission reader store is required")
	}
	return &SubmissionReader{store: store, contestPolicy: contestPolicy, sources: sources}
}

func (s *SubmissionReader) GetSubmission(ctx context.Context, actor auth.Actor, id int64) (SubmissionView, error) {
	record, err := s.store.GetSubmission(ctx, id)
	if err != nil {
		return SubmissionView{}, err
	}
	if !canInspectSubmissions(actor) && (!actor.Authenticated() || actor.UserID != record.UserID) {
		return SubmissionView{}, apperror.Forbidden("submission.not_allowed", "submission access denied")
	}
	return s.submissionView(ctx, actor, record)
}

// GetSubmissionSource reads the stored source for an authorized reader. The
// owner and holders of judge.inspect always pass; anyone else needs contest
// staff rights on the submission's contest, asked through the same contest
// policy that owns result visibility.
func (s *SubmissionReader) GetSubmissionSource(ctx context.Context, actor auth.Actor, id int64) (SubmissionSource, error) {
	record, err := s.store.GetSubmission(ctx, id)
	if err != nil {
		return SubmissionSource{}, err
	}
	if err := s.authorizeSourceRead(ctx, actor, record); err != nil {
		return SubmissionSource{}, err
	}
	if s.sources == nil {
		return SubmissionSource{}, apperror.ServiceUnavailable("submission source storage unavailable")
	}
	artifact, err := s.store.GetArtifact(ctx, record.SourceArtifactID)
	if err != nil {
		return SubmissionSource{}, err
	}
	source, err := s.sources.Get(ctx, artifact.StorageKey)
	if err != nil {
		return SubmissionSource{}, err
	}
	return SubmissionSource{SourceCode: string(source), LanguageID: record.LanguageID}, nil
}

func (s *SubmissionReader) authorizeSourceRead(ctx context.Context, actor auth.Actor, record SubmissionRecord) error {
	if canInspectSubmissions(actor) || (actor.Authenticated() && actor.UserID == record.UserID) {
		return nil
	}
	if record.ContestID == nil {
		return sourceDenied()
	}
	policy, ok := s.contestPolicy.(ContestSourcePolicy)
	if !ok {
		return sourceDenied()
	}
	allowed, err := policy.CanReadSubmissionSource(ctx, actor, contestSubmissionVisibility(record))
	if err != nil {
		return err
	}
	if !allowed {
		return sourceDenied()
	}
	return nil
}

func sourceDenied() error {
	return apperror.Forbidden("submission.source_not_allowed", "submission source access denied")
}

func (s *SubmissionReader) ListSubmissions(ctx context.Context, actor auth.Actor, input ListSubmissionsInput) ([]SubmissionView, int64, error) {
	if !actor.Authenticated() {
		return nil, 0, apperror.Unauthorized("auth_required", "authentication required")
	}
	if !canInspectSubmissions(actor) {
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
	if !canInspectSubmissions(actor) {
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
	return s.submissionCursorPage(ctx, actor, records, limit)
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
	return s.submissionCursorPage(ctx, actor, records, input.Limit)
}

func (s *SubmissionReader) submissionCursorPage(ctx context.Context, actor auth.Actor, records []SubmissionRecord, limit int32) (SubmissionCursorPage, error) {
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

func (s *SubmissionReader) submissionListViews(ctx context.Context, actor auth.Actor, records []SubmissionRecord) ([]SubmissionView, error) {
	visibilities, err := s.submissionListVisibilities(ctx, actor, records)
	if err != nil {
		return nil, err
	}
	attemptIDs := make([]int64, 0, len(records))
	for _, record := range records {
		if terminalStatus(record.Status) && visibilities[record.ID].ShowResult && record.EffectiveAttemptID != nil {
			attemptIDs = append(attemptIDs, *record.EffectiveAttemptID)
		}
	}
	var summaries map[int64]SubmissionListSummary
	if len(attemptIDs) > 0 {
		summaries, err = s.store.ListSubmissionSummaries(ctx, attemptIDs)
		if err != nil {
			return nil, err
		}
	}
	views := make([]SubmissionView, 0, len(records))
	for _, record := range records {
		visibility := visibilities[record.ID]
		view := SubmissionView{Submission: record, Visibility: visibility.Visibility}
		if terminalStatus(record.Status) && visibility.ShowResult && record.EffectiveAttemptID != nil {
			applySubmissionSummary(&view, visibility, summaries[*record.EffectiveAttemptID])
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

	if !terminalStatus(record.Status) || !visibility.ShowResult || record.EffectiveAttemptID == nil {
		return view, nil
	}
	summaries, err := s.store.ListSubmissionSummaries(ctx, []int64{*record.EffectiveAttemptID})
	if err != nil {
		return SubmissionView{}, err
	}
	applySubmissionSummary(&view, visibility, summaries[*record.EffectiveAttemptID])
	if visibility.ShowCases && view.Result != nil {
		view.Cases, err = s.store.ListJudgeCaseResults(ctx, view.Result.AttemptID)
		if err != nil {
			return SubmissionView{}, err
		}
	}
	return view, nil
}

func applySubmissionSummary(view *SubmissionView, visibility SubmissionResultVisibility, summary SubmissionListSummary) {
	if summary.Result == nil || summary.EffectiveAttempt == nil || summary.Result.SubmissionID != view.Submission.ID {
		return
	}
	view.Result = summary.Result
	applyEffectiveOutcome(&view.Submission, *summary.EffectiveAttempt)
	if visibility.ShowAdminDiagnostics {
		view.AdminDiagnostics = summary.EffectiveAttempt
	}
}

func applyEffectiveOutcome(record *SubmissionRecord, attempt JudgeAttemptRecord) {
	record.TimeMS = attempt.TimeMS
	record.MemoryKB = attempt.MemoryKB
	record.ErrorMessage = publicAttemptError(attempt)
}

func (s *SubmissionReader) submissionListVisibilities(ctx context.Context, actor auth.Actor, records []SubmissionRecord) (map[int64]SubmissionResultVisibility, error) {
	visibilities := make(map[int64]SubmissionResultVisibility, len(records))
	contestSubmissions := make([]ContestSubmissionVisibility, 0, len(records))
	for _, record := range records {
		visibilities[record.ID] = SubmissionResultVisibility{ShowResult: true, ShowCases: true, ShowAdminDiagnostics: canInspectSubmissions(actor), Visibility: "visible"}
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
			if canInspectSubmissions(actor) {
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
	visibility := SubmissionResultVisibility{ShowResult: true, ShowCases: true, ShowAdminDiagnostics: canInspectSubmissions(actor), Visibility: "visible"}
	if record.ContestID == nil {
		return visibility, nil
	}
	if s.contestPolicy != nil {
		policyVisibility, err := s.contestPolicy.SubmissionResultVisibility(ctx, actor, contestSubmissionVisibility(record))
		if err != nil {
			return SubmissionResultVisibility{}, err
		}
		if canInspectSubmissions(actor) {
			policyVisibility.ShowAdminDiagnostics = true
		}
		return policyVisibility, nil
	}
	if !canInspectSubmissions(actor) {
		visibility.ShowCases = false
	}
	return visibility, nil
}
