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
	AdmitRun(context.Context, AdmitRunInput) (RunRecord, error)
	GetRun(context.Context, int64) (RunRecord, error)
	UpdateRunStatus(context.Context, int64, judge.Result) (RunRecord, error)
	DeleteArtifact(context.Context, int64) error
}

// RunService creates self-runs and reports their outcome.
//
// There is one run lifecycle and two ways to execute it. An inline service holds
// an engine and runs the code in this process (the local:// endpoint, for
// single-node deployments). A queued service holds no engine: AdmitRun writes a
// judge task in the same transaction as the run, the worker dispatches it, and
// the judge-agent executes it. Both modes then converge on the same thing -- the
// run row reaching a terminal status -- which is also what the client polls.
//
// The mode is not a flag next to an engine that could disagree with it: the
// presence of the engine is the decision, and it is read in exactly one place.
type RunService struct {
	store       runStore
	problems    problem.Reader
	sourceStore sourceStorage
	// runner is nil in queued mode. See the type comment.
	runner     runExecutor
	now        func() time.Time
	runWait    time.Duration
	runTimeout time.Duration
	maxStdin   int
	maxPerUser int
	runCtx     context.Context
	runCancel  context.CancelFunc
	runSlots   chan struct{}
	runMu      sync.Mutex
	runClosing bool
	runWG      sync.WaitGroup
	runClose   sync.Once
	runDone    chan struct{}
}

type RunServiceOptions struct {
	Store         runStore
	ProblemReader problem.Reader
	SourceStore   sourceStorage
	// Runner executes runs inside this process. Nil means runs are enqueued for
	// the judge-agent instead, which is the production path: the API process
	// must not run untrusted code, and only the judge-agent holds a sandbox.
	Runner  runExecutor
	Now     func() time.Time
	Wait    time.Duration
	Timeout time.Duration
	Context context.Context
	// Parallelism caps inline executions in this process. It is irrelevant in
	// queued mode, where the agent owns capacity.
	Parallelism int
	// MaxRunsPerUser caps in-flight runs for a single user, counted in the
	// database. It exists because the agent's capacity is shared: without a
	// per-user gate one caller clicking repeatedly can drain every slot and
	// stall problem pages too.
	MaxRunsPerUser int
	// MaxStdinBytes bounds the stdin a run may carry.
	MaxStdinBytes int
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
	maxStdin := options.MaxStdinBytes
	if maxStdin <= 0 {
		maxStdin = defaultRunStdinMaxBytes
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
		maxStdin:    maxStdin,
		maxPerUser:  maxPerUser,
		runCtx:      runCtx,
		runCancel:   cancel,
		runSlots:    make(chan struct{}, parallelism),
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
	if len(input.Stdin) > s.maxStdin {
		return CreateRunOutput{}, apperror.Unprocessable("run.stdin_too_large", "stdin exceeds the maximum allowed size")
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

	// Inline mode runs the code here, so it takes a slot of this process's pool
	// before doing any work. Queued mode hands the run to the agent, whose
	// capacity is the agent's concern. Either way the per-user cap is enforced
	// by AdmitRun, which counts rows rather than requests.
	inline := s.runner != nil
	reserved := false
	if inline {
		if err := s.reserveExecution(); err != nil {
			return CreateRunOutput{}, err
		}
		reserved = true
		defer func() {
			if reserved {
				s.releaseExecution()
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
	if inline {
		status = StatusRunning
	}
	run, err := s.store.AdmitRun(ctx, AdmitRunInput{
		Run: RunRecord{
			UserID:           actor.UserID,
			ProblemID:        input.ProblemID,
			LanguageID:       input.LanguageID,
			Status:           status,
			SourceArtifactID: artifact.ID,
			Stdin:            input.Stdin,
		},
		MaxActive: s.maxPerUser,
		Enqueue:   !inline,
		NextRunAt: s.now(),
	})
	if err != nil {
		// Admission is decided after the source is uploaded, because the object
		// key and the row are created together. A refusal therefore has to undo
		// the upload: otherwise every rejected run leaves an object that nothing
		// references and no other query can find.
		s.discardSource(ctx, artifact)
		return CreateRunOutput{}, err
	}
	if inline {
		// The slot now belongs to the execution, not to this request.
		go s.completeRunAsync(run.ID, language, input.Source, input.Stdin)
		reserved = false
	}
	return s.awaitRun(ctx, run)
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

// discardSource undoes an upload that admission refused.
//
// The object goes before the row, the same order the retention sweep uses: a
// failure then leaves the row that points at the object, which the orphan sweep
// can still find. The reverse would leave an object nothing can find.
//
// It runs on a context detached from the request, because the request is exactly
// what is being refused -- and it is best-effort on purpose: the run is already
// rejected, so a failure here must not turn into a different error.
func (s *RunService) discardSource(ctx context.Context, artifact ArtifactRecord) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultRunFinalizeTimeout)
	defer cancel()
	_ = s.sourceStore.Delete(cleanupCtx, artifact.StorageKey)
	_ = s.store.DeleteArtifact(cleanupCtx, artifact.ID)
}

// awaitRun gives a fast run the chance to finish before the response is written,
// so the common case answers with the result instead of a 202 the client has to
// poll for. It waits on the row rather than on a goroutine because the row is
// where both execution modes report completion -- and where the client polls
// too, so there is only ever one notion of "finished".
func (s *RunService) awaitRun(ctx context.Context, run RunRecord) (CreateRunOutput, error) {
	if terminalStatus(run.Status) {
		return CreateRunOutput{Run: run}, nil
	}
	deadline := time.Now().Add(s.runWait)
	ticker := time.NewTicker(runAwaitPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return CreateRunOutput{}, ctx.Err()
		case <-ticker.C:
		}
		current, err := s.store.GetRun(ctx, run.ID)
		if err != nil {
			return CreateRunOutput{}, err
		}
		if terminalStatus(current.Status) || time.Now().After(deadline) {
			return CreateRunOutput{Run: current}, nil
		}
	}
}

// completeRunAsync executes one admitted run in this process. It only runs in
// inline mode; the queued mode's executor is the judge-agent.
func (s *RunService) completeRunAsync(runID int64, language LanguageRecord, source []byte, stdin string) {
	defer s.releaseExecution()

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
	// Finalising uses a fresh context: the run context is already canceled when
	// the run timed out, and a timeout still has a result to record.
	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), defaultRunFinalizeTimeout)
	defer finalizeCancel()
	_, _ = s.store.UpdateRunStatus(finalizeCtx, runID, result)
}

// Close stops accepting inline executions and waits for admitted runs to finish.
// Queued runs need no draining: they are already the agent's work.
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

// reserveExecution admits one inline execution through this process's slot pool.
// The per-user cap is not checked here: AdmitRun owns it, because an in-process
// counter cannot see a run that the agent is still executing.
func (s *RunService) reserveExecution() error {
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

func (s *RunService) releaseExecution() {
	<-s.runSlots
	s.runWG.Done()
}
