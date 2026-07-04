package submission

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"SOJ/internal/judge"
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
	return &Worker{repo: options.Repository, queue: options.Queue, engine: options.Judge, problems: options.ProblemReader, testcases: options.TestcaseResolver, store: options.SourceStore, maxAttempts: maxAttempts, backoff: backoff, now: now}
}

type taskPayload struct {
	TaskID       int64 `json:"task_id"`
	SubmissionID int64 `json:"submission_id"`
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
		payload, err := json.Marshal(taskPayload{TaskID: task.ID, SubmissionID: task.SubmissionID})
		if err != nil {
			return dispatched, err
		}
		streamID, err := w.queue.Publish(ctx, task.ID, payload)
		if err != nil {
			_, _ = w.repo.RetryJudgeTask(ctx, task.ID, w.now().Add(w.backoff(task.Attempts)), err.Error())
			return dispatched, err
		}
		if _, err := w.repo.MarkJudgeTaskDispatched(ctx, task.ID, streamID); err != nil {
			return dispatched, err
		}
		dispatched++
	}
	return dispatched, nil
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
	task, err := w.repo.GetJudgeTask(ctx, message.TaskID)
	if err != nil {
		return err
	}
	if task.Status == "done" || task.Status == "dead" {
		return w.queue.Ack(ctx, message.ID)
	}

	submission, err := w.repo.GetSubmission(ctx, task.SubmissionID)
	if err != nil {
		return w.retryOrDead(ctx, message, task, err)
	}
	if terminalStatus(submission.Status) {
		if _, err := w.repo.MarkJudgeTaskDone(ctx, task.ID); err != nil {
			return err
		}
		return w.queue.Ack(ctx, message.ID)
	}
	if _, err := w.repo.MarkJudgeTaskRunning(ctx, task.ID); err != nil {
		return err
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
		return err
	}
	return w.queue.Ack(ctx, message.ID)
}

func (w *Worker) retryOrDead(ctx context.Context, message queue.Message, task JudgeTaskRecord, cause error) error {
	reason := cause.Error()
	if task.Attempts >= w.maxAttempts {
		if _, err := w.repo.MarkJudgeTaskDead(ctx, task.ID, reason); err != nil {
			return err
		}
		if _, err := w.repo.MarkSubmissionSystemError(ctx, task.SubmissionID, reason); err != nil {
			return err
		}
		if err := w.queue.DeadLetter(ctx, message, reason); err != nil {
			return w.queue.Ack(ctx, message.ID)
		}
		return w.queue.Ack(ctx, message.ID)
	}
	if _, err := w.repo.RetryJudgeTask(ctx, task.ID, w.now().Add(w.backoff(task.Attempts)), reason); err != nil {
		return err
	}
	if _, err := w.repo.MarkSubmissionQueued(ctx, task.SubmissionID, reason); err != nil {
		return err
	}
	return w.queue.Ack(ctx, message.ID)
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
