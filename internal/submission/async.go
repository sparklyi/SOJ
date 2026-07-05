package submission

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"SOJ/internal/judge"
	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/queue"
)

type ResultPublisher interface {
	PublishResult(ctx context.Context, event judgeevents.ResultEvent) (string, error)
}

type FakeAsyncAgentOptions struct {
	Judge           judge.JudgeEngine
	SourceStore     SourceStore
	ResultPublisher ResultPublisher
	Now             func() time.Time
}

type FakeAsyncAgent struct {
	judge           judge.JudgeEngine
	sourceStore     SourceStore
	resultPublisher ResultPublisher
	now             func() time.Time
}

func NewFakeAsyncAgent(options FakeAsyncAgentOptions) *FakeAsyncAgent {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FakeAsyncAgent{judge: options.Judge, sourceStore: options.SourceStore, resultPublisher: options.ResultPublisher, now: now}
}

func (a *FakeAsyncAgent) ProcessRequestMessage(ctx context.Context, message queue.Message, requestQueue queue.TaskQueue) error {
	var request judgeevents.RequestEvent
	if err := json.Unmarshal(message.Payload, &request); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	source, err := a.sourceStore.Get(ctx, request.SourceArtifact.StorageKey)
	if err != nil {
		return err
	}
	result, err := a.judge.Judge(ctx, judge.Request{
		LanguageID: request.LanguageID,
		Source:     source,
		Timeout:    time.Duration(request.TimeoutMS) * time.Millisecond,
	})
	if err != nil {
		return err
	}
	result, err = judgeevents.NormalizeResult(result)
	if err != nil {
		return err
	}
	judgedAt := result.JudgedAt
	if judgedAt.IsZero() {
		judgedAt = a.now()
		result.JudgedAt = judgedAt
	}
	if _, err := a.resultPublisher.PublishResult(ctx, judgeevents.ResultEvent{
		ProtocolVersion: judgeevents.ResultEventType,
		EventID:         fmt.Sprintf("%s:result", request.EventID),
		RequestEventID:  request.EventID,
		AttemptID:       request.AttemptID,
		TraceID:         request.TraceID,
		Status:          result.Verdict,
		Result:          result,
		JudgedAt:        judgedAt,
	}); err != nil {
		return err
	}
	return requestQueue.Ack(ctx, message.ID)
}

type ResultConsumerOptions struct {
	Repository ResultConsumerRepository
}

type ResultConsumer struct {
	repo ResultConsumerRepository
}

type ResultConsumerRepository interface {
	CompleteJudgeAttemptResult(ctx context.Context, input CompleteJudgeAttemptResultInput) (SubmissionRecord, bool, error)
}

type CompleteJudgeAttemptResultInput struct {
	EventID        string
	RequestEventID string
	AttemptKey     string
	TraceID        string
	Status         judge.Verdict
	Result         judge.Result
}

type EnsureJudgeAttemptInput struct {
	AttemptID       string
	SubmissionID    int64
	TaskID          int64
	LanguageID      int64
	TestcaseSetID   int64
	TestcaseSetHash string
	ProtocolVersion string
	JudgeEngine     string
	TraceID         string
	StartedAt       time.Time
}

func NewResultConsumer(options ResultConsumerOptions) *ResultConsumer {
	return &ResultConsumer{repo: options.Repository}
}

func (c *ResultConsumer) ProcessResultMessage(ctx context.Context, message queue.Message, resultQueue queue.TaskQueue) error {
	var event judgeevents.ResultEvent
	if err := json.Unmarshal(message.Payload, &event); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	result, err := judgeevents.NormalizeResult(event.Result)
	if err != nil {
		return err
	}
	status, err := judgeevents.NormalizeVerdict(event.Status)
	if err != nil {
		return err
	}
	if result.Verdict == "" {
		result.Verdict = status
	}
	if result.JudgedAt.IsZero() {
		result.JudgedAt = event.JudgedAt
	}
	if _, _, err := c.repo.CompleteJudgeAttemptResult(ctx, CompleteJudgeAttemptResultInput{
		EventID:        event.EventID,
		RequestEventID: event.RequestEventID,
		AttemptKey:     event.AttemptID,
		TraceID:        event.TraceID,
		Status:         status,
		Result:         result,
	}); err != nil {
		return err
	}
	return resultQueue.Ack(ctx, message.ID)
}
