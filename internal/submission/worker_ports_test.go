package submission

import (
	"context"
	"errors"
	"testing"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/problem"
	"SOJ/internal/queue"
)

type taskDispatchStoreStub struct{}

type testcaseSnapshotResolverStub struct{}

func (testcaseSnapshotResolverStub) ReadyTestcaseSet(_ context.Context, problemID, testcaseSetID int64) (problem.TestcaseSet, error) {
	return problem.TestcaseSet{ID: testcaseSetID, ProblemID: problemID}, nil
}

func (testcaseSnapshotResolverStub) ReadyTestcaseMetadata(_ context.Context, problemID, testcaseSetID int64) (testcaseMetadata, error) {
	return testcaseMetadata{ID: testcaseSetID, StorageKey: "cases.zip", ChecksumSHA256: "cases-hash", CaseCount: 1}, nil
}

type taskPublisherStub struct{}

func (taskPublisherStub) Publish(context.Context, int64, []byte) (string, error) {
	return "1-0", nil
}

type messageAckerStub struct {
	acked []string
}

func (q *messageAckerStub) Ack(_ context.Context, messageID string) error {
	q.acked = append(q.acked, messageID)
	return nil
}

type deadLetterQueueStub struct {
	acked []string
	dead  []queue.Message
}

func (q *deadLetterQueueStub) Ack(_ context.Context, messageID string) error {
	q.acked = append(q.acked, messageID)
	return nil
}

func (q *deadLetterQueueStub) DeadLetter(_ context.Context, message queue.Message, _ string) error {
	q.dead = append(q.dead, message)
	return nil
}

type taskQueueConsumerStub struct{}

func (taskQueueConsumerStub) Ensure(context.Context) error {
	return nil
}

func (taskQueueConsumerStub) Consume(context.Context, int, time.Duration) ([]queue.Message, error) {
	return nil, nil
}

func (taskDispatchStoreStub) ClaimPendingJudgeTasks(context.Context, int32) ([]JudgeTaskRecord, error) {
	return nil, nil
}

func (taskDispatchStoreStub) RetryJudgeTask(context.Context, int64, time.Time, string) (JudgeTaskRecord, error) {
	return JudgeTaskRecord{}, nil
}

func (taskDispatchStoreStub) MarkJudgeTaskDispatched(context.Context, int64, string) (JudgeTaskRecord, error) {
	return JudgeTaskRecord{}, nil
}

func (taskDispatchStoreStub) GetSubmission(context.Context, int64) (SubmissionRecord, error) {
	return SubmissionRecord{}, nil
}

func (taskDispatchStoreStub) GetArtifact(context.Context, int64) (ArtifactRecord, error) {
	return ArtifactRecord{}, nil
}

func (taskDispatchStoreStub) GetEnabledLanguage(context.Context, int64) (LanguageRecord, error) {
	return LanguageRecord{}, nil
}

func (taskDispatchStoreStub) MarkSubmissionRunning(context.Context, int64) (SubmissionRecord, error) {
	return SubmissionRecord{}, nil
}

func (taskDispatchStoreStub) EnsureJudgeAttempt(context.Context, EnsureJudgeAttemptInput) (JudgeAttemptRecord, error) {
	return JudgeAttemptRecord{}, nil
}

func TestTaskDispatcherUsesOnlyDispatchStore(t *testing.T) {
	dispatcher := NewTaskDispatcher(TaskDispatcherOptions{
		Store:            taskDispatchStoreStub{},
		Queue:            taskPublisherStub{},
		TestcaseResolver: testcaseSnapshotResolverStub{},
	})

	count, err := dispatcher.DispatchPending(t.Context(), 1)
	if err != nil {
		t.Fatalf("DispatchPending() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("DispatchPending() count = %d, want 0", count)
	}
}

type taskFailureStoreStub struct{}

func (taskFailureStoreStub) RetryJudgeTask(_ context.Context, id int64, _ time.Time, _ string) (JudgeTaskRecord, error) {
	return JudgeTaskRecord{ID: id, Status: "pending"}, nil
}

func (taskFailureStoreStub) MarkJudgeTaskDead(_ context.Context, id int64, _ string) (JudgeTaskRecord, error) {
	return JudgeTaskRecord{ID: id, Status: "dead"}, nil
}

func (taskFailureStoreStub) MarkSubmissionSystemError(_ context.Context, id int64, _ string) (SubmissionRecord, error) {
	return SubmissionRecord{ID: id, Status: StatusSystemErr}, nil
}

func (taskFailureStoreStub) MarkSubmissionQueued(_ context.Context, id int64, _ string) (SubmissionRecord, error) {
	return SubmissionRecord{ID: id, Status: StatusQueued}, nil
}

func TestTaskFailureHandlerUsesOnlyFailureStore(t *testing.T) {
	taskQueue := &deadLetterQueueStub{}
	handler := NewTaskFailureHandler(taskFailureStoreStub{}, taskQueue, 2, nil, nil)

	result, err := handler.retryOrDead(t.Context(), queue.Message{ID: "1-0", TaskID: 1}, JudgeTaskRecord{ID: 1, SubmissionID: 2}, errors.New("judge unavailable"))
	if err != nil {
		t.Fatalf("retryOrDead() error = %v", err)
	}
	if result != "retry" || len(taskQueue.acked) != 1 {
		t.Fatalf("retryOrDead() = %q, acked = %v", result, taskQueue.acked)
	}
}

type taskProcessStoreStub struct{}

func (taskProcessStoreStub) GetJudgeTask(_ context.Context, id int64) (JudgeTaskRecord, error) {
	return JudgeTaskRecord{ID: id, Status: "done"}, nil
}

func (taskProcessStoreStub) GetSubmission(context.Context, int64) (SubmissionRecord, error) {
	return SubmissionRecord{}, nil
}

func (taskProcessStoreStub) MarkJudgeTaskDone(context.Context, int64) (JudgeTaskRecord, error) {
	return JudgeTaskRecord{}, nil
}

func (taskProcessStoreStub) MarkJudgeTaskRunning(context.Context, int64) (JudgeTaskRecord, error) {
	return JudgeTaskRecord{}, nil
}

func (taskProcessStoreStub) MarkSubmissionRunning(context.Context, int64) (SubmissionRecord, error) {
	return SubmissionRecord{}, nil
}

func (taskProcessStoreStub) GetArtifact(context.Context, int64) (ArtifactRecord, error) {
	return ArtifactRecord{}, nil
}

func (taskProcessStoreStub) GetEnabledLanguage(context.Context, int64) (LanguageRecord, error) {
	return LanguageRecord{}, nil
}

func (taskProcessStoreStub) CompleteSubmissionWithResult(context.Context, int64, judge.Result, int32) (SubmissionRecord, error) {
	return SubmissionRecord{}, nil
}

type sourceReaderStub struct{}

func (sourceReaderStub) Get(context.Context, string) ([]byte, error) {
	return []byte("package main"), nil
}

func TestTaskProcessorUsesOnlyProcessStore(t *testing.T) {
	failureQueue := &deadLetterQueueStub{}
	failures := NewTaskFailureHandler(taskFailureStoreStub{}, failureQueue, 0, nil, nil)
	taskQueue := &messageAckerStub{}
	processor := NewTaskProcessor(TaskProcessorOptions{
		Store:            taskProcessStoreStub{},
		Failures:         failures,
		Queue:            taskQueue,
		Judge:            judgeRunnerStub{},
		ProblemReader:    submissionCreatorProblemReaderStub{},
		TestcaseResolver: testcaseSnapshotResolverStub{},
		SourceStore:      sourceReaderStub{},
	})

	if err := processor.ProcessMessage(t.Context(), queue.Message{ID: "1-0", TaskID: 1}); err != nil {
		t.Fatalf("ProcessMessage() error = %v", err)
	}
	if len(taskQueue.acked) != 1 {
		t.Fatalf("ProcessMessage() acked = %v, want one message", taskQueue.acked)
	}
}

func TestWorkerUsesOnlyQueueConsumer(t *testing.T) {
	dispatcher := NewTaskDispatcher(TaskDispatcherOptions{
		Store:            taskDispatchStoreStub{},
		Queue:            taskPublisherStub{},
		TestcaseResolver: testcaseSnapshotResolverStub{},
	})
	failures := NewTaskFailureHandler(taskFailureStoreStub{}, &deadLetterQueueStub{}, 0, nil, nil)
	processor := NewTaskProcessor(TaskProcessorOptions{
		Store:            taskProcessStoreStub{},
		Failures:         failures,
		Queue:            &messageAckerStub{},
		Judge:            judgeRunnerStub{},
		ProblemReader:    submissionCreatorProblemReaderStub{},
		TestcaseResolver: testcaseSnapshotResolverStub{},
		SourceStore:      sourceReaderStub{},
	})
	worker := NewWorker(dispatcher, processor, taskQueueConsumerStub{})

	processed, err := worker.ConsumeOnce(t.Context(), 1, 0)
	if err != nil {
		t.Fatalf("ConsumeOnce() error = %v", err)
	}
	if processed != 0 {
		t.Fatalf("ConsumeOnce() processed = %d, want 0", processed)
	}
}
