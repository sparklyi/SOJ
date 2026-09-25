package problem

import (
	"context"
	"errors"
	"strings"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

type problemReaderStore interface {
	GetProblem(ctx context.Context, id int64) (ProblemRecord, error)
	ListProblems(ctx context.Context, filter ListProblemsFilter) ([]ProblemRecord, error)
	ListProblemsByCursor(ctx context.Context, filter ListProblemsFilter) ([]ProblemRecord, error)
	CountProblems(ctx context.Context, filter ListProblemsFilter) (int64, error)
	GetCurrentProblemStatement(ctx context.Context, problemID int64) (Statement, error)
	ListProblemTags(ctx context.Context, problemID int64) ([]Tag, error)
	GetCurrentReadyTestcaseSet(ctx context.Context, problemID int64) (TestcaseSetRecord, error)
	GetLatestCompletedProblemCheckRun(ctx context.Context, problemID, statementID, testcaseSetID int64) (ProblemCheckRunRecord, error)
	ListProblemCheckFindings(ctx context.Context, runID int64) ([]ProblemCheckFindingRecord, error)
	GetProblemStats(ctx context.Context, problemID int64) (ProblemStats, error)
	ListProblemSubmissionCounts(ctx context.Context, problemIDs []int64) (map[int64]ProblemSubmissionCounts, error)
}

// ProblemReader serves all non-mutating problem workflows.
type ProblemReader struct {
	store    problemReaderStore
	archives testcaseArchiveReader
}

// NewProblemReader builds a reader with its query store and testcase archive source.
// It panics if store is nil.
func NewProblemReader(store problemReaderStore, archives testcaseArchiveReader) *ProblemReader {
	if store == nil {
		panic("problem reader store is required")
	}
	return &ProblemReader{store: store, archives: archives}
}

func (r *ProblemReader) GetProblem(ctx context.Context, actor auth.Actor, id int64) (ProblemRecord, error) {
	// 站点策略：题目是公共资产。`canReadProblem` 已放行 published+public，
	// 匿名访客因此只读得到公开题；私有/未发布题对非 owner/admin 仍是 404。
	p, err := r.store.GetProblem(ctx, id)
	if err != nil {
		return ProblemRecord{}, err
	}
	if err := canReadProblem(actor, p); err != nil {
		return ProblemRecord{}, err
	}
	records := []ProblemRecord{p}
	if err := r.fillSubmissionCounts(ctx, records); err != nil {
		return ProblemRecord{}, err
	}
	return records[0], nil
}

func (r *ProblemReader) ListProblems(ctx context.Context, actor auth.Actor, filter ListProblemsFilter) (ProblemList, error) {
	// 匿名与普通用户都只看到 published+public；owner/admin 由 normalizeListFilter 扩展。
	filter = normalizeListFilter(actor, filter)
	items, err := r.store.ListProblems(ctx, filter)
	if err != nil {
		return ProblemList{}, err
	}
	total, err := r.store.CountProblems(ctx, filter)
	if err != nil {
		return ProblemList{}, err
	}
	if err := r.fillSubmissionCounts(ctx, items); err != nil {
		return ProblemList{}, err
	}
	responses := make([]ProblemResponse, 0, len(items))
	for _, item := range items {
		response, err := r.ProblemResponse(ctx, item)
		if err != nil {
			return ProblemList{}, err
		}
		responses = append(responses, response)
	}
	return ProblemList{Items: responses, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (r *ProblemReader) ListProblemsByCursor(ctx context.Context, actor auth.Actor, filter ListProblemsFilter) (ProblemCursorPage, error) {
	filter = normalizeListFilter(actor, filter)
	limit := filter.PageSize
	cursor := ProblemCursor{
		CreatedAt: time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC),
		ID:        1<<63 - 1,
	}
	if filter.Cursor != nil {
		if filter.Cursor.ID <= 0 || filter.Cursor.CreatedAt.IsZero() {
			return ProblemCursorPage{}, apperror.BadRequest("invalid_cursor", "cursor is invalid")
		}
		cursor = ProblemCursor{CreatedAt: filter.Cursor.CreatedAt.UTC(), ID: filter.Cursor.ID}
	}
	filter.Cursor = &cursor
	filter.Limit = limit + 1
	filter.Offset = 0
	items, err := r.store.ListProblemsByCursor(ctx, filter)
	if err != nil {
		return ProblemCursorPage{}, err
	}
	hasMore := len(items) > int(limit)
	if hasMore {
		items = items[:limit]
	}
	if err := r.fillSubmissionCounts(ctx, items); err != nil {
		return ProblemCursorPage{}, err
	}
	responses := make([]ProblemResponse, 0, len(items))
	for _, item := range items {
		response, err := r.ProblemResponse(ctx, item)
		if err != nil {
			return ProblemCursorPage{}, err
		}
		responses = append(responses, response)
	}
	page := ProblemCursorPage{Items: responses}
	if hasMore {
		last := items[len(items)-1]
		page.NextCursor = &ProblemCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, nil
}

func (r *ProblemReader) GetProblemAuthoringState(ctx context.Context, actor auth.Actor, id int64) (ProblemAuthoringState, error) {
	p, err := r.store.GetProblem(ctx, id)
	if err != nil {
		return ProblemAuthoringState{}, err
	}
	if err := canWriteProblem(actor, p); err != nil {
		return ProblemAuthoringState{}, err
	}
	response, err := r.ProblemResponse(ctx, p)
	if err != nil {
		return ProblemAuthoringState{}, err
	}
	readiness, err := loadProblemAuthoringReadiness(ctx, r.store, id)
	if err != nil {
		return ProblemAuthoringState{}, err
	}
	return ProblemAuthoringState{
		Problem:     response,
		Statement:   readiness.statement,
		TestcaseSet: readiness.testcaseSet,
		LatestCheck: readiness.latestCheck,
		Publishable: len(readiness.blockers) == 0,
		Blockers:    readiness.blockers,
	}, nil
}

func (r *ProblemReader) CurrentStatement(ctx context.Context, actor auth.Actor, problemID int64) (Statement, error) {
	p, err := r.store.GetProblem(ctx, problemID)
	if err != nil {
		return Statement{}, err
	}
	if err := canReadProblem(actor, p); err != nil {
		return Statement{}, err
	}
	return r.store.GetCurrentProblemStatement(ctx, problemID)
}

// fillSubmissionCounts projects submission totals onto a page of problems with a
// single batched query. It is deliberately not part of the problem row query:
// the cursor read is keyset paginated and must stay free of aggregates.
func (r *ProblemReader) fillSubmissionCounts(ctx context.Context, items []ProblemRecord) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
	}
	counts, err := r.store.ListProblemSubmissionCounts(ctx, ids)
	if err != nil {
		return err
	}
	for i := range items {
		if count, ok := counts[items[i].ID]; ok {
			items[i].SubmissionCount = count.SubmissionCount
			items[i].AcceptedCount = count.AcceptedCount
		}
	}
	return nil
}

func (r *ProblemReader) ProblemResponse(ctx context.Context, p ProblemRecord) (ProblemResponse, error) {
	tags, err := r.store.ListProblemTags(ctx, p.ID)
	if err != nil {
		return ProblemResponse{}, err
	}
	tagNames := make([]string, 0, len(tags))
	for _, tag := range tags {
		tagNames = append(tagNames, tag.Name)
	}
	return ProblemResponse{
		ID:         p.ID,
		Title:      p.Title,
		Slug:       p.Slug,
		Difficulty: p.Difficulty,
		Visibility: p.Visibility,
		Status:     p.Status,
		Tags:       tagNames,
		Limits: ProblemLimits{
			TimeLimitMS:   p.TimeLimitMS,
			MemoryLimitKB: p.MemoryLimitKB,
		},
		SubmissionCount: p.SubmissionCount,
		AcceptedCount:   p.AcceptedCount,
		OwnerUserID:     p.OwnerUserID,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
		PublishedAt:     p.PublishedAt,
	}, nil
}

func (r *ProblemReader) CurrentReadyTestcaseSet(ctx context.Context, problemID int64) (TestcaseSet, error) {
	set, err := r.store.GetCurrentReadyTestcaseSet(ctx, problemID)
	if err != nil {
		return TestcaseSet{}, err
	}
	if r.archives == nil {
		return TestcaseSet{}, apperror.ServiceUnavailable("testcase object storage unavailable")
	}
	if strings.TrimSpace(set.StorageKey) == "" {
		return TestcaseSet{}, apperror.BadRequest("testcase.archive_missing", "testcase archive storage key is missing")
	}
	p, err := r.store.GetProblem(ctx, problemID)
	if err != nil {
		return TestcaseSet{}, err
	}
	body, _, err := r.archives.Get(ctx, set.StorageKey)
	if err != nil {
		return TestcaseSet{}, err
	}
	data, err := readAllAndClose(body, defaultMaxTestcaseArchiveBytes)
	if err != nil {
		var resourceErr *testcaseArchiveResourceError
		if errors.As(err, &resourceErr) {
			return TestcaseSet{}, testcaseArchiveBadRequest(err)
		}
		return TestcaseSet{}, err
	}
	cases, err := ParseTestcaseArchive(data, TestcaseArchiveOptions{
		ExpectedCaseCount: set.CaseCount,
		ExpectedSHA256:    set.ChecksumSHA256,
		TimeLimit:         time.Duration(p.TimeLimitMS) * time.Millisecond,
		MemoryKB:          int64(p.MemoryLimitKB),
	})
	if err != nil {
		return TestcaseSet{}, err
	}
	return TestcaseSet{
		ID:        set.ID,
		ProblemID: set.ProblemID,
		Version:   int(set.Version),
		Status:    set.Status,
		Cases:     cases,
	}, nil
}

func (r *ProblemReader) GetForJudge(ctx context.Context, problemID int64) (Problem, error) {
	p, err := r.store.GetProblem(ctx, problemID)
	if err != nil {
		return Problem{}, err
	}
	if p.Status != StatusPublished || p.CurrentStatementID == 0 || p.CurrentTestcaseSetID == 0 || p.CurrentTestcaseStatus != TestcaseStatusReady {
		return Problem{}, apperror.NotFound("problem.not_ready", "problem is not ready for judge")
	}
	return Problem{
		ID:                   p.ID,
		Slug:                 p.Slug,
		Title:                p.Title,
		Visibility:           p.Visibility,
		OwnerUserID:          p.OwnerUserID,
		CurrentStatementID:   p.CurrentStatementID,
		CurrentTestcaseSetID: p.CurrentTestcaseSetID,
	}, nil
}

func (r *ProblemReader) AuthorizeProblemRejudge(ctx context.Context, actor auth.Actor, id int64) error {
	p, err := r.store.GetProblem(ctx, id)
	if err != nil {
		return err
	}
	if err := (RBACProblemPolicy{}).CanRejudge(actor); err != nil {
		return err
	}
	if p.Status != StatusPublished {
		return apperror.Conflict("problem.not_published", "only published problems can be rejudged")
	}
	return nil
}

func (r *ProblemReader) Stats(ctx context.Context, actor auth.Actor, problemID int64) (ProblemStats, error) {
	p, err := r.store.GetProblem(ctx, problemID)
	if err != nil {
		return ProblemStats{}, err
	}
	if err := canReadProblem(actor, p); err != nil {
		return ProblemStats{}, err
	}
	stats, err := r.store.GetProblemStats(ctx, problemID)
	if err != nil {
		return ProblemStats{}, err
	}
	if stats.TotalSubmissions > 0 {
		stats.AcceptanceRate = float64(stats.AcceptedSubmissions) / float64(stats.TotalSubmissions)
	}
	return stats, nil
}
