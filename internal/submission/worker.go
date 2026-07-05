package submission

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"SOJ/internal/judge"
	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/problem"
	"SOJ/internal/queue"
)

type Worker struct {
	repo        Repository
	queue       queue.TaskQueue
	engine      judge.JudgeEngine
	problems    problem.Reader
	testcases   problem.TestcaseResolver
	store       SourceStore
	metrics     WorkerMetrics
	maxAttempts int32
	backoff     func(int32) time.Duration
	now         func() time.Time
}

type WorkerOptions struct {
	Repository       Repository
	Queue            queue.TaskQueue
	Judge            judge.JudgeEngine
	ProblemReader    problem.Reader
	TestcaseResolver problem.TestcaseResolver
	SourceStore      SourceStore
	MaxAttempts      int32
	Backoff          func(int32) time.Duration
	Now              func() time.Time
	Metrics          WorkerMetrics
}

type WorkerMetrics interface {
	RecordJudgeTaskDispatch(result string)
	RecordJudgeTaskProcess(result string, duration time.Duration)
}

func NewWorker(options WorkerOptions) *Worker {
	maxAttempts := options.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	backoff := options.Backoff
	if backoff == nil {
		backoff = defaultJudgeTaskBackoff
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Worker{repo: options.Repository, queue: options.Queue, engine: options.Judge, problems: options.ProblemReader, testcases: options.TestcaseResolver, store: options.SourceStore, metrics: options.Metrics, maxAttempts: maxAttempts, backoff: backoff, now: now}
}

func (w *Worker) DispatchPending(ctx context.Context, limit int32) (int, error) {
	if limit <= 0 {
		limit = 16
	}
	tasks, err := w.repo.ClaimPendingJudgeTasks(ctx, limit)
	if err != nil {
		return 0, err
	}
	dispatched := 0
	for _, task := range tasks {
		event, err := w.requestEvent(ctx, task)
		if err != nil {
			return dispatched, err
		}
		payload, err := json.Marshal(event)
		if err != nil {
			return dispatched, err
		}
		streamID, err := w.queue.Publish(ctx, task.ID, payload)
		if err != nil {
			w.recordDispatch("error")
			_, _ = w.repo.RetryJudgeTask(ctx, task.ID, w.now().Add(w.backoff(task.Attempts)), err.Error())
			return dispatched, err
		}
		if _, err := w.repo.MarkJudgeTaskDispatched(ctx, task.ID, streamID); err != nil {
			w.recordDispatch("error")
			return dispatched, err
		}
		w.recordDispatch("success")
		dispatched++
	}
	return dispatched, nil
}

func (w *Worker) requestEvent(ctx context.Context, task JudgeTaskRecord) (judgeevents.RequestEvent, error) {
	submission, err := w.repo.GetSubmission(ctx, task.SubmissionID)
	if err != nil {
		return judgeevents.RequestEvent{}, err
	}
	artifact, err := w.repo.GetArtifact(ctx, submission.SourceArtifactID)
	if err != nil {
		return judgeevents.RequestEvent{}, err
	}
	language, err := w.repo.GetEnabledLanguage(ctx, submission.LanguageID)
	if err != nil {
		return judgeevents.RequestEvent{}, err
	}
	testcaseSet, err := w.readyTestcaseSet(ctx, submission.ProblemID, submission.TestcaseSetID)
	if err != nil {
		return judgeevents.RequestEvent{}, err
	}
	testcases := make([]judgeevents.TestcaseRef, 0, len(testcaseSet.Cases))
	for i, tc := range testcaseSet.Cases {
		testcases = append(testcases, judgeevents.TestcaseRef{
			Index:             i + 1,
			InputKey:          tc.InputKey,
			ExpectedOutputKey: tc.OutputKey,
			TimeLimitMS:       tc.TimeLimit.Milliseconds(),
			MemoryKB:          tc.MemoryKB,
		})
	}
	now := w.now()
	if submission.Status == StatusQueued {
		if _, err := w.repo.MarkSubmissionRunning(ctx, submission.ID); err != nil {
			return judgeevents.RequestEvent{}, err
		}
	}
	attempt, err := w.repo.EnsureJudgeAttempt(ctx, EnsureJudgeAttemptInput{
		SubmissionID:    submission.ID,
		TaskID:          task.ID,
		LanguageID:      language.ID,
		TestcaseSetID:   testcaseSet.ID,
		TestcaseSetHash: fmt.Sprintf("testcase-set-%d", testcaseSet.ID),
		ProtocolVersion: judgeevents.RequestEventType,
		JudgeEngine:     judge.EngineSOJAgent,
		TraceID:         fmt.Sprintf("trace-submission-%d-task-%d", submission.ID, task.ID),
		StartedAt:       now,
	})
	if err != nil {
		return judgeevents.RequestEvent{}, err
	}
	attemptID := strconv.FormatInt(attempt.ID, 10)
	event := judgeevents.RequestEvent{
		ProtocolVersion: judgeevents.RequestEventType,
		EventID:         fmt.Sprintf("judge-request-%s", attemptID),
		AttemptID:       attemptID,
		TraceID:         valueOr(attempt.TraceID, fmt.Sprintf("trace-submission-%d-task-%d", submission.ID, task.ID)),
		SubmissionID:    submission.ID,
		LanguageID:      language.ID,
		SourceArtifact: judgeevents.ArtifactRef{
			ID:          artifact.ID,
			StorageKey:  artifact.StorageKey,
			ContentHash: artifact.ChecksumSHA256,
		},
		TestcaseSet: judgeevents.TestcaseSetRef{
			ID:   testcaseSet.ID,
			Hash: fmt.Sprintf("testcase-set-%d", testcaseSet.ID),
		},
		Testcases: testcases,
		TimeoutMS: language.DefaultTimeLimit.Milliseconds(),
		MemoryKB:  language.DefaultMemoryKB,
		Priority:  "formal",
		CreatedAt: now,
	}
	if err := event.Validate(); err != nil {
		return judgeevents.RequestEvent{}, err
	}
	return event, nil
}

func valueOr(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}

func (w *Worker) ConsumeOnce(ctx context.Context, limit int, block time.Duration) (int, error) {
	messages, err := w.queue.Consume(ctx, limit, block)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, message := range messages {
		if err := w.ProcessMessage(ctx, message); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (w *Worker) ProcessMessage(ctx context.Context, message queue.Message) error {
	started := time.Now()
	result, err := w.processMessage(ctx, message)
	if err != nil {
		result = "error"
	}
	if result == "" {
		result = "success"
	}
	w.recordProcess(result, time.Since(started))
	return err
}

func (w *Worker) processMessage(ctx context.Context, message queue.Message) (string, error) {
	task, err := w.repo.GetJudgeTask(ctx, message.TaskID)
	if err != nil {
		return "error", err
	}
	if task.Status == "done" || task.Status == "dead" {
		return "skipped", w.queue.Ack(ctx, message.ID)
	}

	submission, err := w.repo.GetSubmission(ctx, task.SubmissionID)
	if err != nil {
		return w.retryOrDead(ctx, message, task, err)
	}
	if terminalStatus(submission.Status) {
		if _, err := w.repo.MarkJudgeTaskDone(ctx, task.ID); err != nil {
			return "error", err
		}
		return "skipped", w.queue.Ack(ctx, message.ID)
	}
	if _, err := w.repo.MarkJudgeTaskRunning(ctx, task.ID); err != nil {
		return "error", err
	}
	if submission.Status == StatusQueued {
		if _, err := w.repo.MarkSubmissionRunning(ctx, submission.ID); err != nil {
			return w.retryOrDead(ctx, message, task, err)
		}
	}
	artifact, err := w.repo.GetArtifact(ctx, submission.SourceArtifactID)
	if err != nil {
		return w.retryOrDead(ctx, message, task, err)
	}
	source, err := w.store.Get(ctx, artifact.StorageKey)
	if err != nil {
		return w.retryOrDead(ctx, message, task, err)
	}
	language, err := w.repo.GetEnabledLanguage(ctx, submission.LanguageID)
	if err != nil {
		return w.retryOrDead(ctx, message, task, err)
	}
	if _, err := w.problems.GetForJudge(ctx, submission.ProblemID); err != nil {
		return w.retryOrDead(ctx, message, task, err)
	}
	testcaseSet, err := w.readyTestcaseSet(ctx, submission.ProblemID, submission.TestcaseSetID)
	if err != nil {
		return w.retryOrDead(ctx, message, task, err)
	}
	testcases := make([]judge.Testcase, 0, len(testcaseSet.Cases))
	for _, tc := range testcaseSet.Cases {
		testcases = append(testcases, judge.Testcase{InputKey: tc.InputKey, ExpectedOutputKey: tc.OutputKey, TimeLimit: tc.TimeLimit, MemoryKB: tc.MemoryKB})
	}

	result, err := w.engine.Judge(ctx, judge.Request{
		LanguageID: language.ID,
		Source:     source,
		Testcases:  testcases,
		Timeout:    language.DefaultTimeLimit,
	})
	if err != nil {
		return w.retryOrDead(ctx, message, task, err)
	}
	service := Service{repo: w.repo}
	if _, err := service.CompleteSubmission(ctx, submission.ID, result); err != nil {
		return w.retryOrDead(ctx, message, task, err)
	}
	if _, err := w.repo.MarkJudgeTaskDone(ctx, task.ID); err != nil {
		return "error", err
	}
	return "success", w.queue.Ack(ctx, message.ID)
}

func (w *Worker) retryOrDead(ctx context.Context, message queue.Message, task JudgeTaskRecord, cause error) (string, error) {
	reason := cause.Error()
	if task.Attempts >= w.maxAttempts {
		if _, err := w.repo.MarkJudgeTaskDead(ctx, task.ID, reason); err != nil {
			return "error", err
		}
		if _, err := w.repo.MarkSubmissionSystemError(ctx, task.SubmissionID, reason); err != nil {
			return "error", err
		}
		if err := w.queue.DeadLetter(ctx, message, reason); err != nil {
			return "dead", w.queue.Ack(ctx, message.ID)
		}
		return "dead", w.queue.Ack(ctx, message.ID)
	}
	if _, err := w.repo.RetryJudgeTask(ctx, task.ID, w.now().Add(w.backoff(task.Attempts)), reason); err != nil {
		return "error", err
	}
	if _, err := w.repo.MarkSubmissionQueued(ctx, task.SubmissionID, reason); err != nil {
		return "error", err
	}
	return "retry", w.queue.Ack(ctx, message.ID)
}

func (w *Worker) Run(ctx context.Context, limit int, block time.Duration) error {
	if err := w.queue.Ensure(ctx); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err := w.DispatchPending(ctx, int32(limit)); err != nil {
			return fmt.Errorf("dispatch pending: %w", err)
		}
		if _, err := w.ConsumeOnce(ctx, limit, block); err != nil {
			return fmt.Errorf("consume: %w", err)
		}
	}
}

type testcaseSnapshotResolver interface {
	ReadyTestcaseSet(ctx context.Context, problemID, testcaseSetID int64) (problem.TestcaseSet, error)
}

func (w *Worker) readyTestcaseSet(ctx context.Context, problemID, testcaseSetID int64) (problem.TestcaseSet, error) {
	if resolver, ok := w.testcases.(testcaseSnapshotResolver); ok {
		return resolver.ReadyTestcaseSet(ctx, problemID, testcaseSetID)
	}
	return problem.TestcaseSet{}, fmt.Errorf("testcase snapshot resolver unavailable for testcase_set_id %d", testcaseSetID)
}

func defaultJudgeTaskBackoff(attempts int32) time.Duration {
	schedule := []time.Duration{
		5 * time.Second,
		30 * time.Second,
		2 * time.Minute,
		10 * time.Minute,
		30 * time.Minute,
	}
	if attempts < 0 {
		attempts = 0
	}
	if int(attempts) >= len(schedule) {
		return schedule[len(schedule)-1]
	}
	return schedule[attempts]
}

func (w *Worker) recordDispatch(result string) {
	if w.metrics != nil {
		w.metrics.RecordJudgeTaskDispatch(result)
	}
}

func (w *Worker) recordProcess(result string, duration time.Duration) {
	if w.metrics != nil {
		w.metrics.RecordJudgeTaskProcess(result, duration)
	}
}
