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
	StatusSystemErr   = "system_error"
	StatusCanceled    = "canceled"

	defaultRunShortWait = 3 * time.Second
	defaultRunTimeout   = 2 * time.Minute
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
}

type ContestSubmissionPolicy interface {
	ValidateSubmission(ctx context.Context, actor auth.Actor, problemID, contestID int64) error
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
	return &Service{repo: options.Repository, problems: options.ProblemReader, testcases: options.TestcaseResolver, queue: options.Queue, store: options.SourceStore, judge: options.Judge, contestPolicy: options.ContestPolicy, terminalHook: options.TerminalHook, now: now, runWait: runWait, runTimeout: runTimeout}
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
	ctx, cancel := context.WithTimeout(context.Background(), s.runTimeout)
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
	run, err := s.repo.UpdateRunStatus(ctx, runID, result)
	if err != nil {
		return
	}
	select {
	case done <- run:
	default:
	}
}

func (s *Service) GetSubmission(ctx context.Context, actor auth.Actor, id int64) (SubmissionRecord, error) {
	record, err := s.repo.GetSubmission(ctx, id)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if !actor.Admin() && (!actor.Authenticated() || actor.UserID != record.UserID) {
		return SubmissionRecord{}, apperror.Forbidden("submission.not_allowed", "submission access denied")
	}
	return record, nil
}

func (s *Service) ListSubmissions(ctx context.Context, actor auth.Actor, input ListSubmissionsInput) ([]SubmissionRecord, int64, error) {
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
	return s.repo.ListSubmissions(ctx, input)
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
	updated, err := s.repo.UpdateSubmissionStatus(ctx, submissionID, result, score)
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
	case StatusAccepted, StatusWrongAnswer, StatusCompileErr, StatusRuntimeErr, StatusTimeLimit, StatusMemoryLimit, StatusSystemErr, StatusCanceled:
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
	case judge.VerdictTimeLimitExceeded:
		return StatusTimeLimit
	case judge.VerdictMemoryLimitExceeded:
		return StatusMemoryLimit
	case judge.VerdictSystemError:
		return StatusSystemErr
	default:
		return StatusSystemErr
	}
}
