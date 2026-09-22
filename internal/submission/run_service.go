package submission

import (
	"context"
	"sync"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/judge"
	"SOJ/internal/problem"
)

type runStore interface {
	GetEnabledLanguage(context.Context, int64) (LanguageRecord, error)
	CreateArtifact(context.Context, ArtifactRecord) (ArtifactRecord, error)
	CreateRun(context.Context, RunRecord) (RunRecord, error)
	GetRun(context.Context, int64) (RunRecord, error)
	UpdateRunStatus(context.Context, int64, judge.Result) (RunRecord, error)
}

// RunService owns direct run creation, execution, and shutdown.
type RunService struct {
	store       runStore
	problems    problem.Reader
	sourceStore sourceWriter
	runner      runExecutor
	now         func() time.Time
	runWait     time.Duration
	runTimeout  time.Duration
	runCtx      context.Context
	runCancel   context.CancelFunc
	runSlots    chan struct{}
	// userRuns counts in-flight runs per user. runMu guards it together with
	// runClosing -- one lock for the admission state, not two.
	userRuns   map[int64]int
	maxPerUser int
	runMu      sync.Mutex
	runClosing bool
	runWG      sync.WaitGroup
	runClose   sync.Once
	runDone    chan struct{}
}

type RunServiceOptions struct {
	Store         runStore
	ProblemReader problem.Reader
	SourceStore   sourceWriter
	Runner        runExecutor
	Now           func() time.Time
	Wait          time.Duration
	Timeout       time.Duration
	Context       context.Context
	Parallelism   int
	// MaxRunsPerUser caps in-flight runs for a single user. It exists because
	// Parallelism is a global pool: without a per-user gate one caller clicking
	// repeatedly can drain every slot and stall problem pages too.
	MaxRunsPerUser int
}

func NewRunService(options RunServiceOptions) *RunService {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	wait := options.Wait
	if wait <= 0 {
		wait = defaultRunShortWait
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultRunTimeout
	}
	parallelism := options.Parallelism
	if parallelism <= 0 {
		parallelism = defaultRunParallelism
	}
	maxPerUser := options.MaxRunsPerUser
	if maxPerUser <= 0 {
		maxPerUser = defaultRunPerUser
	}
	parent := options.Context
	if parent == nil {
		parent = context.Background()
	}
	runCtx, cancel := context.WithCancel(parent)
	service := &RunService{
		store:       options.Store,
		problems:    options.ProblemReader,
		sourceStore: options.SourceStore,
		runner:      options.Runner,
		now:         now,
		runWait:     wait,
		runTimeout:  timeout,
		runCtx:      runCtx,
		runCancel:   cancel,
		runSlots:    make(chan struct{}, parallelism),
		userRuns:    make(map[int64]int),
		maxPerUser:  maxPerUser,
		runDone:     make(chan struct{}),
	}
	if done := parent.Done(); done != nil {
		go func() {
			select {
			case <-done:
				service.beginShutdown()
			case <-service.runDone:
			}
		}()
	}
	return service
}

func (s *RunService) CreateRun(ctx context.Context, actor auth.Actor, input CreateRunInput) (CreateRunOutput, error) {
	if !actor.Authenticated() {
		return CreateRunOutput{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	if len(input.Source) == 0 {
		return CreateRunOutput{}, apperror.BadRequest("source_required", "source is required")
	}
	// A playground run has no problem to validate. Skipping the check here (and
	// not inside a nil-able reader dependency) keeps problem.Reader a required
	// constructor dependency -- a nil interface would only fail at call time.
	if input.ProblemID != nil {
		if _, err := s.problems.GetForJudge(ctx, *input.ProblemID); err != nil {
			return CreateRunOutput{}, err
		}
	}
	language, err := s.store.GetEnabledLanguage(ctx, input.LanguageID)
	if err != nil {
		return CreateRunOutput{}, err
	}
	reservedExecution := false
	if s.runner != nil {
		if err := s.reserveExecution(actor.UserID); err != nil {
			return CreateRunOutput{}, err
		}
		reservedExecution = true
		defer func() {
			if reservedExecution {
				s.releaseExecution(actor.UserID)
			}
		}()
	}
	object, err := s.sourceStore.Put(ctx, "run", actor.UserID, input.Source)
	if err != nil {
		return CreateRunOutput{}, err
	}
	artifact, err := s.store.CreateArtifact(ctx, ArtifactRecord{
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
	if s.runner != nil {
		status = StatusRunning
	}
	run, err := s.store.CreateRun(ctx, RunRecord{
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
	if s.runner == nil {
		return CreateRunOutput{Run: run}, nil
	}

	done := make(chan RunRecord, 1)
	go s.completeRunAsync(run.ID, actor.UserID, language, input.Source, input.Stdin, done)
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

func (s *RunService) GetRun(ctx context.Context, actor auth.Actor, id int64) (RunRecord, error) {
	record, err := s.store.GetRun(ctx, id)
	if err != nil {
		return RunRecord{}, err
	}
	if !actor.Admin() && (!actor.Authenticated() || actor.UserID != record.UserID) {
		return RunRecord{}, apperror.Forbidden("run.not_allowed", "run access denied")
	}
	return record, nil
}

func (s *RunService) CompleteRun(ctx context.Context, runID int64, result judge.Result) (RunRecord, error) {
	current, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return RunRecord{}, err
	}
	if terminalStatus(current.Status) {
		return current, nil
	}
	return s.store.UpdateRunStatus(ctx, runID, result)
}

func (s *RunService) completeRunAsync(runID, userID int64, language LanguageRecord, source []byte, stdin string, done chan<- RunRecord) {
	defer s.releaseExecution(userID)

	ctx, cancel := context.WithTimeout(s.runCtx, s.runTimeout)
	defer cancel()
	result, err := s.runner.Run(ctx, judge.RunRequest{
		LanguageID: language.ID,
		Source:     source,
		Stdin:      stdin,
		Timeout:    language.DefaultTimeLimit,
		MemoryKB:   language.DefaultMemoryKB,
	})
	if err != nil {
		result = judge.Result{Verdict: judge.VerdictSystemError, ErrorMessage: err.Error(), JudgedAt: s.now()}
	}
	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), defaultRunFinalizeTimeout)
	defer finalizeCancel()
	run, err := s.store.UpdateRunStatus(finalizeCtx, runID, result)
	if err != nil {
		return
	}
	select {
	case done <- run:
	default:
	}
}

// Close stops accepting direct run executions and waits for admitted runs to finish.
func (s *RunService) Close(ctx context.Context) error {
	s.beginShutdown()
	select {
	case <-s.runDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *RunService) beginShutdown() {
	s.runClose.Do(func() {
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

// reserveExecution admits one execution through two gates: the per-user
// in-flight cap first, then the global sandbox slot pool. Order matters -- a
// run rejected by the per-user cap must not consume a global slot it would
// then have to hand back.
func (s *RunService) reserveExecution(userID int64) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runClosing || s.runCtx.Err() != nil {
		return apperror.ServiceUnavailable("run execution is shutting down")
	}
	if s.userRuns[userID] >= s.maxPerUser {
		return apperror.TooManyRequests("run.user_limit_exceeded", "too many runs in flight for this user")
	}
	select {
	case s.runSlots <- struct{}{}:
		s.userRuns[userID]++
		s.runWG.Add(1)
		return nil
	default:
		return apperror.ServiceUnavailable("run execution capacity exhausted")
	}
}

func (s *RunService) releaseExecution(userID int64) {
	<-s.runSlots
	s.runMu.Lock()
	// Delete at zero rather than storing it: leaving zero entries behind would
	// grow the map once per user, forever.
	if remaining := s.userRuns[userID] - 1; remaining > 0 {
		s.userRuns[userID] = remaining
	} else {
		delete(s.userRuns, userID)
	}
	s.runMu.Unlock()
	s.runWG.Done()
}
