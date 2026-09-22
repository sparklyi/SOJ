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

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type taskDispatchStore interface {
	ClaimPendingJudgeTasks(context.Context, int32) ([]JudgeTaskRecord, error)
	ClaimPendingRunTasks(context.Context, int32) ([]JudgeTaskRecord, error)
	RetryJudgeTask(context.Context, int64, time.Time, string) (JudgeTaskRecord, error)
	MarkJudgeTaskDispatched(context.Context, int64, string) (JudgeTaskRecord, error)
	GetSubmission(context.Context, int64) (SubmissionRecord, error)
	GetRun(context.Context, int64) (RunRecord, error)
	GetArtifact(context.Context, int64) (ArtifactRecord, error)
	GetEnabledLanguage(context.Context, int64) (LanguageRecord, error)
	MarkSubmissionRunning(context.Context, int64) (SubmissionRecord, error)
	MarkRunRunning(context.Context, int64) (RunRecord, error)
	EnsureJudgeAttempt(context.Context, EnsureJudgeAttemptInput) (JudgeAttemptRecord, error)
}

type taskDispatchMetrics interface {
	RecordJudgeTaskDispatch(result string)
	RecordJudgeRequestPayloadSize(bytes int)
}

// TaskDispatcher turns pending judge tasks into queue messages.
//
// Submissions and runs share the task table and lifecycle but not the stream:
// a run goes to RunQueue so playground traffic never queues in front of a formal
// submission. Only the event construction and the destination differ.
type TaskDispatcher struct {
	store     taskDispatchStore
	queue     taskPublisher
	runQueue  taskPublisher
	testcases testcaseMetadataResolver
	metrics   taskDispatchMetrics
	now       func() time.Time
	backoff   func(int32) time.Duration
}

type TaskDispatcherOptions struct {
	Store            taskDispatchStore
	Queue            taskPublisher
	RunQueue         taskPublisher
	TestcaseResolver testcaseMetadataResolver
	Metrics          taskDispatchMetrics
	Now              func() time.Time
	Backoff          func(int32) time.Duration
}

func NewTaskDispatcher(options TaskDispatcherOptions) *TaskDispatcher {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	backoff := options.Backoff
	if backoff == nil {
		backoff = defaultJudgeTaskBackoff
	}
	return &TaskDispatcher{
		store:     options.Store,
		queue:     options.Queue,
		runQueue:  options.RunQueue,
		testcases: options.TestcaseResolver,
		metrics:   options.Metrics,
		now:       now,
		backoff:   backoff,
	}
}

func (d *TaskDispatcher) DispatchPending(ctx context.Context, limit int32) (int, error) {
	if limit <= 0 {
		limit = 16
	}
	submissions, err := d.dispatchSubmissions(ctx, limit)
	if err != nil {
		return submissions, err
	}
	runs, err := d.dispatchRuns(ctx, limit)
	return submissions + runs, err
}

func (d *TaskDispatcher) dispatchSubmissions(ctx context.Context, limit int32) (int, error) {
	tasks, err := d.store.ClaimPendingJudgeTasks(ctx, limit)
	if err != nil {
		return 0, err
	}
	return d.publish(ctx, tasks, d.queue, d.submissionRequestEvent)
}

func (d *TaskDispatcher) dispatchRuns(ctx context.Context, limit int32) (int, error) {
	if d.runQueue == nil {
		return 0, nil
	}
	tasks, err := d.store.ClaimPendingRunTasks(ctx, limit)
	if err != nil {
		return 0, err
	}
	return d.publish(ctx, tasks, d.runQueue, d.runRequestEvent)
}

// publish is the half of dispatching that does not care what the task is about:
// build the event, publish it, record the stream id, and back off on failure.
type requestEventBuilder func(context.Context, JudgeTaskRecord) (judgeevents.RequestEvent, error)

func (d *TaskDispatcher) publish(ctx context.Context, tasks []JudgeTaskRecord, target taskPublisher, build requestEventBuilder) (int, error) {
	dispatched := 0
	for _, task := range tasks {
		event, err := build(ctx, task)
		if err != nil {
			return dispatched, err
		}
		payload, err := json.Marshal(event)
		if err != nil {
			return dispatched, err
		}
		if d.metrics != nil {
			d.metrics.RecordJudgeRequestPayloadSize(len(payload))
		}
		streamID, err := target.Publish(ctx, task.ID, payload)
		if err != nil {
			d.record("error")
			_, _ = d.store.RetryJudgeTask(ctx, task.ID, d.now().Add(d.backoff(task.Attempts)), err.Error())
			return dispatched, err
		}
		if _, err := d.store.MarkJudgeTaskDispatched(ctx, task.ID, streamID); err != nil {
			d.record("error")
			return dispatched, err
		}
		d.record("success")
		dispatched++
	}
	return dispatched, nil
}

func (d *TaskDispatcher) submissionRequestEvent(ctx context.Context, task JudgeTaskRecord) (judgeevents.RequestEvent, error) {
	if task.SubmissionID == nil {
		return judgeevents.RequestEvent{}, errJudgeTaskIsNotASubmission
	}
	submission, err := d.store.GetSubmission(ctx, *task.SubmissionID)
	if err != nil {
		return judgeevents.RequestEvent{}, err
	}
	artifact, err := d.store.GetArtifact(ctx, submission.SourceArtifactID)
	if err != nil {
		return judgeevents.RequestEvent{}, err
	}
	language, err := d.store.GetEnabledLanguage(ctx, submission.LanguageID)
	if err != nil {
		return judgeevents.RequestEvent{}, err
	}
	testcaseSet, err := d.testcases.ReadyTestcaseMetadata(ctx, submission.ProblemID, submission.TestcaseSetID)
	if err != nil {
		return judgeevents.RequestEvent{}, err
	}
	now := d.now()
	if submission.Status == StatusQueued {
		if _, err := d.store.MarkSubmissionRunning(ctx, submission.ID); err != nil {
			return judgeevents.RequestEvent{}, err
		}
	}
	fallbackTraceID := fmt.Sprintf("trace-submission-%d-task-%d", submission.ID, task.ID)
	traceID, traceContext := traceIdentityFromContext(ctx, fallbackTraceID)
	attempt, err := d.store.EnsureJudgeAttempt(ctx, EnsureJudgeAttemptInput{
		SubmissionID:    &submission.ID,
		TaskID:          task.ID,
		LanguageID:      language.ID,
		TestcaseSetID:   testcaseSet.ID,
		TestcaseSetHash: testcaseSet.ChecksumSHA256,
		ProtocolVersion: judgeevents.RequestEventType,
		JudgeEngine:     judge.EngineSOJAgent,
		TraceID:         traceID,
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
		TraceID:         valueOr(attempt.TraceID, traceID),
		TraceContext:    traceContext,
		SubmissionID:    submission.ID,
		LanguageID:      language.ID,
		LanguageSlug:    language.EngineLanguageID,
		SourceArtifact: judgeevents.ArtifactRef{
			ID:          artifact.ID,
			StorageKey:  artifact.StorageKey,
			ContentHash: artifact.ChecksumSHA256,
		},
		TestcaseSet: judgeevents.TestcaseSetRef{
			ID:             testcaseSet.ID,
			ChecksumSHA256: testcaseSet.ChecksumSHA256,
			StorageKey:     testcaseSet.StorageKey,
			CaseCount:      testcaseSet.CaseCount,
			TimeLimitMS:    testcaseSet.TimeLimit.Milliseconds(),
			MemoryKB:       testcaseSet.MemoryKB,
		},
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

// runRequestEvent builds the request for a self-run. It carries no testcase set
// -- there is nothing to judge, only source plus stdin to execute -- and is
// marked scratch so the agent can prefer formal work if it ever needs to.
func (d *TaskDispatcher) runRequestEvent(ctx context.Context, task JudgeTaskRecord) (judgeevents.RequestEvent, error) {
	if task.RunID == nil {
		return judgeevents.RequestEvent{}, errJudgeTaskIsNotARun
	}
	run, err := d.store.GetRun(ctx, *task.RunID)
	if err != nil {
		return judgeevents.RequestEvent{}, err
	}
	artifact, err := d.store.GetArtifact(ctx, run.SourceArtifactID)
	if err != nil {
		return judgeevents.RequestEvent{}, err
	}
	language, err := d.store.GetEnabledLanguage(ctx, run.LanguageID)
	if err != nil {
		return judgeevents.RequestEvent{}, err
	}
	now := d.now()
	if run.Status == StatusQueued {
		if _, err := d.store.MarkRunRunning(ctx, run.ID); err != nil {
			return judgeevents.RequestEvent{}, err
		}
	}
	fallbackTraceID := fmt.Sprintf("trace-run-%d-task-%d", run.ID, task.ID)
	traceID, traceContext := traceIdentityFromContext(ctx, fallbackTraceID)
	attempt, err := d.store.EnsureJudgeAttempt(ctx, EnsureJudgeAttemptInput{
		RunID:           &run.ID,
		TaskID:          task.ID,
		LanguageID:      language.ID,
		ProtocolVersion: judgeevents.RequestEventType,
		JudgeEngine:     judge.EngineSOJAgent,
		TraceID:         traceID,
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
		TraceID:         valueOr(attempt.TraceID, traceID),
		TraceContext:    traceContext,
		RunID:           run.ID,
		LanguageID:      language.ID,
		LanguageSlug:    language.EngineLanguageID,
		SourceArtifact: judgeevents.ArtifactRef{
			ID:          artifact.ID,
			StorageKey:  artifact.StorageKey,
			ContentHash: artifact.ChecksumSHA256,
		},
		Stdin:     run.Stdin,
		TimeoutMS: language.DefaultTimeLimit.Milliseconds(),
		MemoryKB:  language.DefaultMemoryKB,
		Priority:  judgeevents.PriorityScratch,
		CreatedAt: now,
	}
	if err := event.Validate(); err != nil {
		return judgeevents.RequestEvent{}, err
	}
	return event, nil
}

func (d *TaskDispatcher) record(result string) {
	if d.metrics != nil {
		d.metrics.RecordJudgeTaskDispatch(result)
	}
}

type taskProcessStore interface {
	GetJudgeTask(context.Context, int64) (JudgeTaskRecord, error)
	GetSubmission(context.Context, int64) (SubmissionRecord, error)
	MarkJudgeTaskDone(context.Context, int64) (JudgeTaskRecord, error)
	MarkJudgeTaskRunning(context.Context, int64) (JudgeTaskRecord, error)
	MarkSubmissionRunning(context.Context, int64) (SubmissionRecord, error)
	GetArtifact(context.Context, int64) (ArtifactRecord, error)
	GetEnabledLanguage(context.Context, int64) (LanguageRecord, error)
	CompleteSubmissionWithResult(context.Context, int64, judge.Result, int32) (SubmissionRecord, error)
}

type taskFailureStore interface {
	RetryJudgeTask(context.Context, int64, time.Time, string) (JudgeTaskRecord, error)
	MarkJudgeTaskDead(context.Context, int64, string) (JudgeTaskRecord, error)
	MarkSubmissionQueued(context.Context, int64, string) (SubmissionRecord, error)
}

// TaskFailureHandler owns retry and terminal failure transitions.
type TaskFailureHandler struct {
	store       taskFailureStore
	queue       deadLetterQueue
	maxAttempts int32
	backoff     func(int32) time.Duration
	now         func() time.Time
}

func NewTaskFailureHandler(store taskFailureStore, taskQueue deadLetterQueue, maxAttempts int32, backoff func(int32) time.Duration, now func() time.Time) *TaskFailureHandler {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if backoff == nil {
		backoff = defaultJudgeTaskBackoff
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &TaskFailureHandler{store: store, queue: taskQueue, maxAttempts: maxAttempts, backoff: backoff, now: now}
}

func (h *TaskFailureHandler) retryOrDead(ctx context.Context, message queue.Message, task JudgeTaskRecord, cause error) (string, error) {
	reason := cause.Error()
	if task.Attempts >= h.maxAttempts {
		if _, err := h.store.MarkJudgeTaskDead(ctx, task.ID, reason); err != nil {
			return "error", err
		}
		if err := h.queue.DeadLetter(ctx, message, reason); err != nil {
			return "dead", h.queue.Ack(ctx, message.ID)
		}
		return "dead", h.queue.Ack(ctx, message.ID)
	}
	if _, err := h.store.RetryJudgeTask(ctx, task.ID, h.now().Add(h.backoff(task.Attempts)), reason); err != nil {
		return "error", err
	}
	if _, err := h.store.MarkSubmissionQueued(ctx, *task.SubmissionID, reason); err != nil {
		return "error", err
	}
	return "retry", h.queue.Ack(ctx, message.ID)
}

type taskProcessMetrics interface {
	RecordJudgeTaskProcess(result string, duration time.Duration)
}

// TaskProcessor executes a single judge queue message.
type TaskProcessor struct {
	store     taskProcessStore
	failures  *TaskFailureHandler
	queue     MessageAcker
	engine    judgeRunner
	problems  problem.Reader
	testcases testcaseSnapshotResolver
	sources   sourceReader
	metrics   taskProcessMetrics
}

type TaskProcessorOptions struct {
	Store            taskProcessStore
	Failures         *TaskFailureHandler
	Queue            MessageAcker
	Judge            judgeRunner
	ProblemReader    problem.Reader
	TestcaseResolver testcaseSnapshotResolver
	SourceStore      sourceReader
	Metrics          taskProcessMetrics
}

func NewTaskProcessor(options TaskProcessorOptions) *TaskProcessor {
	return &TaskProcessor{
		store:     options.Store,
		failures:  options.Failures,
		queue:     options.Queue,
		engine:    options.Judge,
		problems:  options.ProblemReader,
		testcases: options.TestcaseResolver,
		sources:   options.SourceStore,
		metrics:   options.Metrics,
	}
}

func (p *TaskProcessor) ProcessMessage(ctx context.Context, message queue.Message) error {
	started := time.Now()
	result, err := p.processMessage(ctx, message)
	if err != nil {
		result = "error"
	}
	if result == "" {
		result = "success"
	}
	if p.metrics != nil {
		p.metrics.RecordJudgeTaskProcess(result, time.Since(started))
	}
	return err
}

func (p *TaskProcessor) processMessage(ctx context.Context, message queue.Message) (string, error) {
	task, err := p.store.GetJudgeTask(ctx, message.TaskID)
	if err != nil {
		return "error", err
	}
	if task.Status == "done" || task.Status == "dead" {
		return "skipped", p.queue.Ack(ctx, message.ID)
	}
	// TaskProcessor is the in-process judging path: it loads testcases and judges.
	// Runs are executed by the judge-agent instead, so a run task that shows up
	// here (only possible if an operator points both streams at the same queue)
	// is acked and left alone. A stuck run is recovered by ResetStaleTasks and
	// MarkStaleRuns, both of which are subject-agnostic.
	if task.RunID != nil {
		return "skipped", p.queue.Ack(ctx, message.ID)
	}

	submission, err := p.store.GetSubmission(ctx, *task.SubmissionID)
	if err != nil {
		return p.failures.retryOrDead(ctx, message, task, err)
	}
	if terminalStatus(submission.Status) {
		if _, err := p.store.MarkJudgeTaskDone(ctx, task.ID); err != nil {
			return "error", err
		}
		return "skipped", p.queue.Ack(ctx, message.ID)
	}
	if _, err := p.store.MarkJudgeTaskRunning(ctx, task.ID); err != nil {
		return "error", err
	}
	if submission.Status == StatusQueued {
		if _, err := p.store.MarkSubmissionRunning(ctx, submission.ID); err != nil {
			return p.failures.retryOrDead(ctx, message, task, err)
		}
	}
	artifact, err := p.store.GetArtifact(ctx, submission.SourceArtifactID)
	if err != nil {
		return p.failures.retryOrDead(ctx, message, task, err)
	}
	source, err := p.sources.Get(ctx, artifact.StorageKey)
	if err != nil {
		return p.failures.retryOrDead(ctx, message, task, err)
	}
	language, err := p.store.GetEnabledLanguage(ctx, submission.LanguageID)
	if err != nil {
		return p.failures.retryOrDead(ctx, message, task, err)
	}
	if _, err := p.problems.GetForJudge(ctx, submission.ProblemID); err != nil {
		return p.failures.retryOrDead(ctx, message, task, err)
	}
	testcaseSet, err := p.testcases.ReadyTestcaseSet(ctx, submission.ProblemID, submission.TestcaseSetID)
	if err != nil {
		return p.failures.retryOrDead(ctx, message, task, err)
	}
	testcases := make([]judge.Testcase, 0, len(testcaseSet.Cases))
	for _, testcase := range testcaseSet.Cases {
		testcases = append(testcases, judge.Testcase{InputKey: testcase.InputKey, ExpectedOutputKey: testcase.OutputKey, TimeLimit: testcase.TimeLimit, MemoryKB: testcase.MemoryKB})
	}

	result, err := p.engine.Judge(ctx, judge.Request{
		LanguageID: language.ID,
		Source:     source,
		Testcases:  testcases,
		Timeout:    language.DefaultTimeLimit,
	})
	if err != nil {
		return p.failures.retryOrDead(ctx, message, task, err)
	}
	if _, _, err := completeSubmission(ctx, p.store, submission.ID, result); err != nil {
		return p.failures.retryOrDead(ctx, message, task, err)
	}
	if _, err := p.store.MarkJudgeTaskDone(ctx, task.ID); err != nil {
		return "error", err
	}
	return "success", p.queue.Ack(ctx, message.ID)
}

// Worker composes task dispatching and processing for the worker loop.
type Worker struct {
	dispatcher *TaskDispatcher
	processor  *TaskProcessor
	queue      taskQueueConsumer
}

func NewWorker(dispatcher *TaskDispatcher, processor *TaskProcessor, taskQueue taskQueueConsumer) *Worker {
	return &Worker{dispatcher: dispatcher, processor: processor, queue: taskQueue}
}

func (w *Worker) DispatchPending(ctx context.Context, limit int32) (int, error) {
	return w.dispatcher.DispatchPending(ctx, limit)
}

func (w *Worker) ConsumeOnce(ctx context.Context, limit int, block time.Duration) (int, error) {
	messages, err := w.queue.Consume(ctx, limit, block)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, message := range messages {
		if err := w.processor.ProcessMessage(ctx, message); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (w *Worker) ProcessMessage(ctx context.Context, message queue.Message) error {
	return w.processor.ProcessMessage(ctx, message)
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

type testcaseMetadataResolver interface {
	ReadyTestcaseMetadata(ctx context.Context, problemID, testcaseSetID int64) (testcaseMetadata, error)
}

func traceIdentityFromContext(ctx context.Context, fallback string) (string, judgeevents.TraceContext) {
	spanContext := trace.SpanFromContext(ctx).SpanContext()
	if !spanContext.IsValid() || !spanContext.TraceID().IsValid() {
		return fallback, judgeevents.TraceContext{}
	}
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return spanContext.TraceID().String(), traceContextFromCarrier(carrier)
}

func traceContextFromCarrier(carrier propagation.MapCarrier) judgeevents.TraceContext {
	return judgeevents.TraceContext{
		Traceparent: carrier.Get("traceparent"),
		Tracestate:  carrier.Get("tracestate"),
	}
}

func valueOr(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
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
