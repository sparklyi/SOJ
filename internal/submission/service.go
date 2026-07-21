package submission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/judge"
	"SOJ/internal/problem"
	"SOJ/internal/queue"
)

const (
	StatusQueued      = "queued"
	StatusRunning     = "running"
	StatusAccepted    = "accepted"
	StatusWrongAnswer = "wrong_answer"
	StatusCompileErr  = "compile_error"
	StatusRuntimeErr  = "runtime_error"
	StatusTimeLimit   = "time_limit"
	StatusMemoryLimit = "memory_limit"
	StatusOutputLimit = "output_limit"
	StatusSystemErr   = "system_error"
	StatusCanceled    = "canceled"

	defaultRunShortWait       = 3 * time.Second
	defaultRunTimeout         = 2 * time.Minute
	defaultRunParallelism     = 1
	defaultRunFinalizeTimeout = 5 * time.Second
)

type SourceObject struct {
	StorageKey     string
	ChecksumSHA256 string
	SizeBytes      int64
	ContentType    string
}

type SourceStore interface {
	Put(ctx context.Context, ownerType string, ownerID int64, source []byte) (SourceObject, error)
	Get(ctx context.Context, storageKey string) ([]byte, error)
}

type MemorySourceStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func NewMemorySourceStore() *MemorySourceStore {
	return &MemorySourceStore{objects: make(map[string][]byte)}
}

func (s *MemorySourceStore) Put(ctx context.Context, ownerType string, ownerID int64, source []byte) (SourceObject, error) {
	sum := sha256.Sum256(source)
	checksum := hex.EncodeToString(sum[:])
	key := fmt.Sprintf("%s/%d/%s", ownerType, ownerID, checksum)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = append([]byte(nil), source...)
	return SourceObject{StorageKey: key, ChecksumSHA256: checksum, SizeBytes: int64(len(source)), ContentType: "text/plain; charset=utf-8"}, nil
}

func (s *MemorySourceStore) Get(ctx context.Context, storageKey string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	source, ok := s.objects[storageKey]
	if !ok {
		return nil, apperror.NotFound("source_not_found", "source artifact not found")
	}
	return append([]byte(nil), source...), nil
}

type Service struct {
	repo          Repository
	problems      problem.Reader
	testcases     problem.TestcaseResolver
	queue         queue.TaskQueue
	store         SourceStore
	judge         judge.JudgeEngine
	contestPolicy ContestSubmissionPolicy
	terminalHook  TerminalHook
	now           func() time.Time
	runWait       time.Duration
	runTimeout    time.Duration
	runCtx        context.Context
	runCancel     context.CancelFunc
	runSlots      chan struct{}
	runMu         sync.Mutex
	runClosing    bool
	runWG         sync.WaitGroup
	runCloseOnce  sync.Once
	runDone       chan struct{}
}

type ServiceOptions struct {
	Repository       Repository
	ProblemReader    problem.Reader
	TestcaseResolver problem.TestcaseResolver
	Queue            queue.TaskQueue
	SourceStore      SourceStore
	Judge            judge.JudgeEngine
	ContestPolicy    ContestSubmissionPolicy
	TerminalHook     TerminalHook
	Now              func() time.Time
	RunWait          time.Duration
	RunTimeout       time.Duration
	RunContext       context.Context
	RunParallelism   int
}

type ContestSubmissionPolicy interface {
	ValidateSubmission(ctx context.Context, actor auth.Actor, problemID, contestID int64) error
}

type ContestResultVisibilityPolicy interface {
	SubmissionResultVisibility(ctx context.Context, actor auth.Actor, submission ContestSubmissionVisibility) (SubmissionResultVisibility, error)
}

type ContestResultVisibilityBatchPolicy interface {
	SubmissionResultVisibilities(ctx context.Context, actor auth.Actor, submissions []ContestSubmissionVisibility) (map[int64]SubmissionResultVisibility, error)
}

type ContestSubmissionVisibility struct {
	ID          int64
	UserID      int64
	ProblemID   int64
	ContestID   int64
	SubmittedAt time.Time
	JudgedAt    *time.Time
}

type SubmissionResultVisibility struct {
	ShowResult           bool
	ShowCases            bool
	ShowAdminDiagnostics bool
	Visibility           string
}

type TerminalHook interface {
	AfterSubmissionTerminal(ctx context.Context, submission TerminalSubmission) error
}

type TerminalSubmission struct {
	SubmissionID int64
	UserID       int64
	ProblemID    int64
	ContestID    *int64
	Status       string
	SubmittedAt  time.Time
	JudgedAt     time.Time
}

func NewService(options ServiceOptions) *Service {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	runWait := options.RunWait
	if runWait <= 0 {
		runWait = defaultRunShortWait
	}
	runTimeout := options.RunTimeout
	if runTimeout <= 0 {
		runTimeout = defaultRunTimeout
	}
	runParallelism := options.RunParallelism
	if runParallelism <= 0 {
		runParallelism = defaultRunParallelism
	}
	runParentCtx := options.RunContext
	if runParentCtx == nil {
		runParentCtx = context.Background()
	}
	runCtx, runCancel := context.WithCancel(runParentCtx)
	service := &Service{
		repo:          options.Repository,
		problems:      options.ProblemReader,
		testcases:     options.TestcaseResolver,
		queue:         options.Queue,
		store:         options.SourceStore,
		judge:         options.Judge,
		contestPolicy: options.ContestPolicy,
		terminalHook:  options.TerminalHook,
		now:           now,
		runWait:       runWait,
		runTimeout:    runTimeout,
		runCtx:        runCtx,
		runCancel:     runCancel,
		runSlots:      make(chan struct{}, runParallelism),
		runDone:       make(chan struct{}),
	}
	if done := runParentCtx.Done(); done != nil {
		go func() {
			select {
			case <-done:
				service.beginRunShutdown()
			case <-service.runDone:
			}
		}()
	}
	return service
}

type CreateSubmissionInput struct {
	ProblemID  int64
	ContestID  *int64
	LanguageID int64
	Source     []byte
}

type CreateSubmissionOutput struct {
	Submission SubmissionRecord
	Task       JudgeTaskRecord
	StreamID   string
}

type SubmissionView struct {
	Submission       SubmissionRecord
	Result           *SubmissionResultRecord
	Cases            []JudgeCaseResultRecord
	AdminDiagnostics *JudgeAttemptRecord
	Visibility       string
}

type ListOwnSubmissionsCursorInput struct {
	Cursor *SubmissionCursor
	Limit  int32
}

type SubmissionCursorPage struct {
	Items      []SubmissionView
	NextCursor *SubmissionCursor
}

func (s *Service) CreateSubmission(ctx context.Context, actor auth.Actor, input CreateSubmissionInput) (CreateSubmissionOutput, error) {
	if !actor.Authenticated() {
		return CreateSubmissionOutput{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	if len(input.Source) == 0 {
		return CreateSubmissionOutput{}, apperror.BadRequest("source_required", "source is required")
	}
	if _, err := s.problems.GetForJudge(ctx, input.ProblemID); err != nil {
		return CreateSubmissionOutput{}, err
	}
	if input.ContestID != nil && s.contestPolicy != nil {
		if err := s.contestPolicy.ValidateSubmission(ctx, actor, input.ProblemID, *input.ContestID); err != nil {
			return CreateSubmissionOutput{}, err
		}
	}
	testcaseSet, err := s.testcases.CurrentReadyTestcaseSet(ctx, input.ProblemID)
	if err != nil {
		return CreateSubmissionOutput{}, err
	}
	if _, err := s.repo.GetEnabledLanguage(ctx, input.LanguageID); err != nil {
		return CreateSubmissionOutput{}, err
	}

	object, err := s.store.Put(ctx, "submission", actor.UserID, input.Source)
	if err != nil {
		return CreateSubmissionOutput{}, err
	}
	artifact, err := s.repo.CreateArtifact(ctx, ArtifactRecord{
		OwnerType:      "submission",
		OwnerID:        actor.UserID,
		Kind:           "source",
		StorageKey:     object.StorageKey,
		ChecksumSHA256: object.ChecksumSHA256,
		SizeBytes:      object.SizeBytes,
		ContentType:    object.ContentType,
	})
	if err != nil {
		return CreateSubmissionOutput{}, err
	}
	sub, task, err := s.repo.CreateSubmissionWithTask(ctx, SubmissionRecord{
		UserID:           actor.UserID,
		ProblemID:        input.ProblemID,
		ContestID:        input.ContestID,
		LanguageID:       input.LanguageID,
		TestcaseSetID:    testcaseSet.ID,
		Status:           StatusQueued,
		SourceArtifactID: artifact.ID,
	}, s.now())
	if err != nil {
		return CreateSubmissionOutput{}, err
	}
	return CreateSubmissionOutput{Submission: sub, Task: task}, nil
}

type CreateRunInput struct {
	ProblemID  int64
	LanguageID int64
	Source     []byte
	Stdin      string
}

type CreateRunOutput struct {
	Run RunRecord
}

func (s *Service) CreateRun(ctx context.Context, actor auth.Actor, input CreateRunInput) (CreateRunOutput, error) {
	if !actor.Authenticated() {
		return CreateRunOutput{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	if len(input.Source) == 0 {
		return CreateRunOutput{}, apperror.BadRequest("source_required", "source is required")
	}
	if _, err := s.problems.GetForJudge(ctx, input.ProblemID); err != nil {
		return CreateRunOutput{}, err
	}
	language, err := s.repo.GetEnabledLanguage(ctx, input.LanguageID)
	if err != nil {
		return CreateRunOutput{}, err
	}
	reservedExecution := false
	if s.judge != nil {
		if err := s.reserveRunExecution(); err != nil {
			return CreateRunOutput{}, err
		}
		reservedExecution = true
		defer func() {
			if reservedExecution {
				s.releaseRunExecution()
			}
		}()
	}
	object, err := s.store.Put(ctx, "run", actor.UserID, input.Source)
	if err != nil {
		return CreateRunOutput{}, err
	}
	artifact, err := s.repo.CreateArtifact(ctx, ArtifactRecord{
		OwnerType:      "run",
		OwnerID:        actor.UserID,
		Kind:           "source",
		StorageKey:     object.StorageKey,
		ChecksumSHA256: object.ChecksumSHA256,
		SizeBytes:      object.SizeBytes,
		ContentType:    object.ContentType,
	})
	if err != nil {
		return CreateRunOutput{}, err
	}
	status := StatusQueued
	if s.judge != nil {
		status = StatusRunning
	}
	run, err := s.repo.CreateRun(ctx, RunRecord{
		UserID:           actor.UserID,
		ProblemID:        input.ProblemID,
		LanguageID:       input.LanguageID,
		Status:           status,
		SourceArtifactID: artifact.ID,
		Stdin:            input.Stdin,
	})
	if err != nil {
		return CreateRunOutput{}, err
	}
	if s.judge == nil {
		return CreateRunOutput{Run: run}, nil
	}

	done := make(chan RunRecord, 1)
	go s.completeRunAsync(run.ID, language, input.Source, input.Stdin, done)
	reservedExecution = false

	timer := time.NewTimer(s.runWait)
	defer timer.Stop()
	select {
	case completed := <-done:
		return CreateRunOutput{Run: completed}, nil
	case <-timer.C:
		return CreateRunOutput{Run: run}, nil
	case <-ctx.Done():
		return CreateRunOutput{}, ctx.Err()
	}
}

func (s *Service) completeRunAsync(runID int64, language LanguageRecord, source []byte, stdin string, done chan<- RunRecord) {
	defer s.releaseRunExecution()

	ctx, cancel := context.WithTimeout(s.runCtx, s.runTimeout)
	defer cancel()

	result, err := s.judge.Judge(ctx, judge.Request{
		LanguageID: language.ID,
		Source:     source,
		Stdin:      stdin,
		Timeout:    language.DefaultTimeLimit,
	})
	if err != nil {
		result = judge.Result{Verdict: judge.VerdictSystemError, ErrorMessage: err.Error(), JudgedAt: s.now()}
	}
	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), defaultRunFinalizeTimeout)
	defer finalizeCancel()
	run, err := s.repo.UpdateRunStatus(finalizeCtx, runID, result)
	if err != nil {
		return
	}
	select {
	case done <- run:
	default:
	}
}

// Close stops accepting direct run executions and waits for admitted runs to finish.
func (s *Service) Close(ctx context.Context) error {
	s.beginRunShutdown()
	select {
	case <-s.runDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) beginRunShutdown() {
	s.runCloseOnce.Do(func() {
		s.runMu.Lock()
		s.runClosing = true
		s.runCancel()
		s.runMu.Unlock()
		go func() {
			s.runWG.Wait()
			close(s.runDone)
		}()
	})
}

func (s *Service) reserveRunExecution() error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runClosing || s.runCtx.Err() != nil {
		return apperror.ServiceUnavailable("run execution is shutting down")
	}
	select {
	case s.runSlots <- struct{}{}:
		s.runWG.Add(1)
		return nil
	default:
		return apperror.ServiceUnavailable("run execution capacity exhausted")
	}
}

func (s *Service) releaseRunExecution() {
	<-s.runSlots
	s.runWG.Done()
}

func (s *Service) GetSubmission(ctx context.Context, actor auth.Actor, id int64) (SubmissionView, error) {
	record, err := s.repo.GetSubmission(ctx, id)
	if err != nil {
		return SubmissionView{}, err
	}
	if !actor.Admin() && (!actor.Authenticated() || actor.UserID != record.UserID) {
		return SubmissionView{}, apperror.Forbidden("submission.not_allowed", "submission access denied")
	}
	return s.submissionView(ctx, actor, record)
}

func (s *Service) ListSubmissions(ctx context.Context, actor auth.Actor, input ListSubmissionsInput) ([]SubmissionView, int64, error) {
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
	records, total, err := s.repo.ListSubmissions(ctx, input)
	if err != nil {
		return nil, 0, err
	}
	views, err := s.submissionListViews(ctx, actor, records)
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

func (s *Service) ListSubmissionsByCursor(ctx context.Context, actor auth.Actor, input ListSubmissionsInput) (SubmissionCursorPage, error) {
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
	records, err := s.repo.ListSubmissionsByCursor(ctx, input)
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

func (s *Service) ListOwnSubmissionsByCursor(ctx context.Context, actor auth.Actor, input ListOwnSubmissionsCursorInput) (SubmissionCursorPage, error) {
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
	records, err := s.repo.ListSubmissionsByUserBefore(ctx, actor.UserID, cursor, input.Limit+1)
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

func (s *Service) submissionListViews(ctx context.Context, actor auth.Actor, records []SubmissionRecord) ([]SubmissionView, error) {
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
	summaries, err := s.repo.ListSubmissionSummaries(ctx, submissionIDs, includeAttempts)
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

func (s *Service) GetRun(ctx context.Context, actor auth.Actor, id int64) (RunRecord, error) {
	record, err := s.repo.GetRun(ctx, id)
	if err != nil {
		return RunRecord{}, err
	}
	if !actor.Admin() && (!actor.Authenticated() || actor.UserID != record.UserID) {
		return RunRecord{}, apperror.Forbidden("run.not_allowed", "run access denied")
	}
	return record, nil
}

func (s *Service) CompleteSubmission(ctx context.Context, submissionID int64, result judge.Result) (SubmissionRecord, error) {
	current, err := s.repo.GetSubmission(ctx, submissionID)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if terminalStatus(current.Status) {
		return current, nil
	}
	score := int32(0)
	if result.Verdict == judge.VerdictAccepted {
		score = 100
	}
	updated, err := s.repo.CompleteSubmissionWithResult(ctx, submissionID, result, score)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if s.terminalHook != nil {
		judgedAt := result.JudgedAt
		if judgedAt.IsZero() {
			judgedAt = s.now()
		}
		if err := s.terminalHook.AfterSubmissionTerminal(ctx, TerminalSubmission{
			SubmissionID: updated.ID,
			UserID:       updated.UserID,
			ProblemID:    updated.ProblemID,
			ContestID:    updated.ContestID,
			Status:       updated.Status,
			SubmittedAt:  updated.SubmittedAt,
			JudgedAt:     judgedAt,
		}); err != nil {
			return updated, err
		}
	}
	return updated, nil
}

func (s *Service) submissionView(ctx context.Context, actor auth.Actor, record SubmissionRecord) (SubmissionView, error) {
	visibility, err := s.submissionVisibility(ctx, actor, record)
	if err != nil {
		return SubmissionView{}, err
	}

	view := SubmissionView{Submission: record, Visibility: visibility.Visibility}
	if !visibility.ShowResult || !terminalStatus(record.Status) {
		return view, nil
	}
	result, err := s.repo.GetSubmissionResult(ctx, record.ID)
	if err != nil {
		if appErr, ok := err.(*apperror.Error); ok && appErr.HTTPStatus == 404 {
			return view, nil
		}
		return SubmissionView{}, err
	}
	view.Result = &result

	attempt, err := s.repo.GetLatestJudgeAttemptBySubmissionID(ctx, record.ID)
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
		cases, err := s.repo.ListJudgeCaseResults(ctx, attempt.ID)
		if err != nil {
			return SubmissionView{}, err
		}
		view.Cases = cases
	}
	return view, nil
}

func (s *Service) submissionListVisibilities(ctx context.Context, actor auth.Actor, records []SubmissionRecord) (map[int64]SubmissionResultVisibility, error) {
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

func (s *Service) submissionVisibility(ctx context.Context, actor auth.Actor, record SubmissionRecord) (SubmissionResultVisibility, error) {
	visibility := SubmissionResultVisibility{ShowResult: true, ShowCases: true, ShowAdminDiagnostics: actor.Admin(), Visibility: "visible"}
	if record.ContestID == nil {
		return visibility, nil
	}
	if policy, ok := s.contestPolicy.(ContestResultVisibilityPolicy); ok {
		policyVisibility, err := policy.SubmissionResultVisibility(ctx, actor, contestSubmissionVisibility(record))
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

func contestSubmissionVisibility(record SubmissionRecord) ContestSubmissionVisibility {
	return ContestSubmissionVisibility{
		ID:          record.ID,
		UserID:      record.UserID,
		ProblemID:   record.ProblemID,
		ContestID:   *record.ContestID,
		SubmittedAt: record.SubmittedAt,
		JudgedAt:    record.JudgedAt,
	}
}

func (s *Service) CompleteRun(ctx context.Context, runID int64, result judge.Result) (RunRecord, error) {
	current, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return RunRecord{}, err
	}
	if terminalStatus(current.Status) {
		return current, nil
	}
	return s.repo.UpdateRunStatus(ctx, runID, result)
}

type ListLanguagesInput struct {
	Enabled *bool
	Engine  *string
	Offset  int32
	Limit   int32
}

type UpdateLanguageInput struct {
	Enabled              *bool
	DefaultTimeLimitMS   *int32
	DefaultMemoryLimitKB *int32
}

func (s *Service) ListLanguages(ctx context.Context, actor auth.Actor, input ListLanguagesInput) ([]LanguageRecord, int64, error) {
	if !actor.Admin() {
		return nil, 0, apperror.Forbidden("admin_required", "admin role required")
	}
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 50
	}
	return s.repo.ListLanguages(ctx, input)
}

func (s *Service) ListPublicLanguages(ctx context.Context, actor auth.Actor, input ListLanguagesInput) ([]LanguageRecord, int64, error) {
	enabled := true
	input.Enabled = &enabled
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 50
	}
	return s.repo.ListLanguages(ctx, input)
}

func (s *Service) SyncLanguages(ctx context.Context, actor auth.Actor) ([]LanguageRecord, error) {
	if !actor.Root() {
		return nil, apperror.Forbidden("root_required", "root role required")
	}
	languages, err := s.judge.Languages(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]LanguageRecord, 0, len(languages))
	for _, language := range languages {
		record, err := s.repo.UpsertLanguage(ctx, language)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

func (s *Service) UpdateLanguage(ctx context.Context, actor auth.Actor, id int64, input UpdateLanguageInput) (LanguageRecord, error) {
	if !actor.Admin() {
		return LanguageRecord{}, apperror.Forbidden("admin_required", "admin role required")
	}
	return s.repo.UpdateLanguage(ctx, id, input)
}

func terminalStatus(status string) bool {
	switch status {
	case StatusAccepted, StatusWrongAnswer, StatusCompileErr, StatusRuntimeErr, StatusTimeLimit, StatusMemoryLimit, StatusOutputLimit, StatusSystemErr, StatusCanceled:
		return true
	default:
		return false
	}
}

func dbStatus(verdict judge.Verdict) string {
	switch verdict {
	case judge.VerdictAccepted:
		return StatusAccepted
	case judge.VerdictWrongAnswer:
		return StatusWrongAnswer
	case judge.VerdictCompileError:
		return StatusCompileErr
	case judge.VerdictRuntimeError:
		return StatusRuntimeErr
	case judge.VerdictTimeLimitExceeded, judge.VerdictTimeLimit:
		return StatusTimeLimit
	case judge.VerdictMemoryLimitExceeded, judge.VerdictMemoryLimit:
		return StatusMemoryLimit
	case judge.VerdictOutputLimit:
		return StatusOutputLimit
	case judge.VerdictSystemError:
		return StatusSystemErr
	case judge.VerdictCanceled:
		return StatusCanceled
	default:
		return StatusSystemErr
	}
}
