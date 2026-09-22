package submission

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"SOJ/internal/judge"
	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/judgecore"
	"SOJ/internal/queue"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type ResultPublisher interface {
	PublishResult(ctx context.Context, event judgeevents.ResultEvent) (string, error)
}

type FakeAsyncAgentOptions struct {
	Judge judgeRunner
	// Run executes self-runs. It is separate from Judge because the two are
	// different operations; an agent must be able to do both, but a caller only
	// ever exercises one.
	Run             runExecutor
	SourceStore     sourceReader
	ResultPublisher ResultPublisher
	Now             func() time.Time
}

type FakeAsyncAgent struct {
	judge           judgeRunner
	run             runExecutor
	sourceStore     sourceReader
	resultPublisher ResultPublisher
	now             func() time.Time
}

// CoreJudge is the judgecore operation set the agent needs: judge a submission
// against testcases, or execute a self-run. Two methods, mirroring the two
// request shapes, so neither can be served by the other by accident.
type CoreJudge interface {
	Judge(ctx context.Context, request judgecore.Request) (judge.Result, error)
	Run(ctx context.Context, request judge.RunRequest) (judge.Result, error)
}

type CoreAsyncAgentOptions struct {
	Core            CoreJudge
	SourceStore     sourceReader
	TestcaseLoader  testcaseLoader
	ResultPublisher ResultPublisher
	Now             func() time.Time
}

type CoreAsyncAgent struct {
	core            CoreJudge
	sourceStore     sourceReader
	testcaseLoader  testcaseLoader
	resultPublisher ResultPublisher
	now             func() time.Time
}

func NewFakeAsyncAgent(options FakeAsyncAgentOptions) *FakeAsyncAgent {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FakeAsyncAgent{judge: options.Judge, run: options.Run, sourceStore: options.SourceStore, resultPublisher: options.ResultPublisher, now: now}
}

func NewCoreAsyncAgent(options CoreAsyncAgentOptions) *CoreAsyncAgent {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &CoreAsyncAgent{core: options.Core, sourceStore: options.SourceStore, testcaseLoader: options.TestcaseLoader, resultPublisher: options.ResultPublisher, now: now}
}

func (a *FakeAsyncAgent) ProcessRequestMessage(ctx context.Context, message queue.Message, requestQueue MessageAcker) error {
	var request judgeevents.RequestEvent
	if err := json.Unmarshal(message.Payload, &request); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	ctx = contextWithTraceContext(ctx, request.TraceContext)
	source, err := a.sourceStore.Get(ctx, request.SourceArtifact.StorageKey)
	if err != nil {
		return err
	}
	// A run executes source against stdin; a submission is judged against a
	// testcase set. Two operations, not one with an empty set -- which is why the
	// agent branches here rather than building zero cases and calling Judge.
	var result judge.Result
	if request.RunID != 0 {
		if a.run == nil {
			return fmt.Errorf("run engine is required to serve run requests")
		}
		result, err = a.run.Run(ctx, judge.RunRequest{
			LanguageID: request.LanguageID,
			Source:     source,
			Stdin:      request.Stdin,
			Timeout:    time.Duration(request.TimeoutMS) * time.Millisecond,
			MemoryKB:   request.MemoryKB,
		})
	} else {
		result, err = a.judge.Judge(ctx, judge.Request{
			LanguageID: request.LanguageID,
			Source:     source,
			Timeout:    time.Duration(request.TimeoutMS) * time.Millisecond,
		})
	}
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
		TraceContext:    request.TraceContext,
		Status:          result.Verdict,
		Result:          result,
		JudgedAt:        judgedAt,
	}); err != nil {
		return err
	}
	return requestQueue.Ack(ctx, message.ID)
}

func (a *CoreAsyncAgent) ProcessRequestMessage(ctx context.Context, message queue.Message, requestQueue MessageAcker) error {
	var request judgeevents.RequestEvent
	if err := json.Unmarshal(message.Payload, &request); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	ctx = contextWithTraceContext(ctx, request.TraceContext)
	source, err := a.sourceStore.Get(ctx, request.SourceArtifact.StorageKey)
	if err != nil {
		return err
	}
	// See FakeAsyncAgent: a run and a submission are different operations, so the
	// agent branches before it reaches judgecore.
	var result judge.Result
	if request.RunID != 0 {
		result, err = a.core.Run(ctx, judge.RunRequest{
			LanguageID: request.LanguageID,
			Source:     source,
			Stdin:      request.Stdin,
			Timeout:    time.Duration(request.TimeoutMS) * time.Millisecond,
			MemoryKB:   request.MemoryKB,
		})
	} else {
		result, err = a.judgeSubmission(ctx, request, source)
	}
	if err != nil {
		return err
	}
	result.Manifest.TraceID = request.TraceID
	if request.SubmissionID != 0 {
		result.Manifest.TestcaseSetHash = request.TestcaseSet.ChecksumSHA256
		attachTestcaseKeys(request, result.Cases)
	}
	if err := publishAsyncResult(ctx, a.resultPublisher, request, result, a.now); err != nil {
		return err
	}
	return requestQueue.Ack(ctx, message.ID)
}

// judgeSubmission loads the testcase set and judges against it. Kept separate so
// the run path never reaches the loader: loading testcases for a run is the bug
// this split prevents.
func (a *CoreAsyncAgent) judgeSubmission(ctx context.Context, request judgeevents.RequestEvent, source []byte) (judge.Result, error) {
	if a.testcaseLoader == nil {
		return judge.Result{}, fmt.Errorf("testcase loader is required")
	}
	testcaseSet, err := a.testcaseLoader.Load(ctx, request.TestcaseSet)
	if err != nil {
		return judge.Result{}, err
	}
	cases := make([]judgecore.Case, 0, len(testcaseSet))
	for i, item := range testcaseSet {
		cases = append(cases, judgecore.Case{
			Index:          i + 1,
			Input:          item.InputKey,
			ExpectedOutput: item.OutputKey,
			TimeLimit:      item.TimeLimit,
			MemoryKB:       item.MemoryKB,
		})
	}
	return a.core.Judge(ctx, judgecore.Request{
		LanguageID: request.LanguageID,
		Source:     source,
		Cases:      cases,
		Timeout:    time.Duration(request.TimeoutMS) * time.Millisecond,
		MemoryKB:   request.MemoryKB,
	})
}

func attachTestcaseKeys(request judgeevents.RequestEvent, cases []judge.CaseResult) {
	for i := range cases {
		if cases[i].TestcaseKey != "" {
			continue
		}
		index := cases[i].Index
		if index == 0 {
			index = i + 1
		}
		cases[i].TestcaseKey = fmt.Sprintf("testcase-set-%d/case-%d", request.TestcaseSet.ID, index)
	}
}

func publishAsyncResult(ctx context.Context, publisher ResultPublisher, request judgeevents.RequestEvent, result judge.Result, now func() time.Time) error {
	result, err := judgeevents.NormalizeResult(result)
	if err != nil {
		return err
	}
	judgedAt := result.JudgedAt
	if judgedAt.IsZero() {
		judgedAt = now()
		result.JudgedAt = judgedAt
	}
	_, err = publisher.PublishResult(ctx, judgeevents.ResultEvent{
		ProtocolVersion: judgeevents.ResultEventType,
		EventID:         fmt.Sprintf("%s:result", request.EventID),
		RequestEventID:  request.EventID,
		AttemptID:       request.AttemptID,
		TraceID:         request.TraceID,
		TraceContext:    request.TraceContext,
		Status:          result.Verdict,
		Result:          result,
		JudgedAt:        judgedAt,
	})
	return err
}

type ResultConsumer struct {
	store resultConsumerStore
}

type resultConsumerStore interface {
	// CompleteJudgeAttemptResult reports whether the result was persisted. The
	// subject -- submission or run -- follows from the attempt, so the consumer
	// does not need to know which it is handling.
	CompleteJudgeAttemptResult(ctx context.Context, input CompleteJudgeAttemptResultInput) (bool, error)
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
	AttemptID string
	// Exactly one of SubmissionID / RunID identifies the subject, mirroring the
	// judge_attempts columns. A run also has no testcase set: executing source
	// against stdin is the whole operation.
	SubmissionID    *int64
	RunID           *int64
	TaskID          int64
	LanguageID      int64
	TestcaseSetID   int64
	TestcaseSetHash string
	ProtocolVersion string
	JudgeEngine     string
	TraceID         string
	StartedAt       time.Time
}

func NewResultConsumer(store resultConsumerStore) *ResultConsumer {
	return &ResultConsumer{store: store}
}

func (c *ResultConsumer) ProcessResultMessage(ctx context.Context, message queue.Message, resultQueue MessageAcker) error {
	var event judgeevents.ResultEvent
	if err := json.Unmarshal(message.Payload, &event); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	ctx = contextWithTraceContext(ctx, event.TraceContext)
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
	if _, err := c.store.CompleteJudgeAttemptResult(ctx, CompleteJudgeAttemptResultInput{
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

func contextWithTraceContext(ctx context.Context, traceContext judgeevents.TraceContext) context.Context {
	if traceContext.Empty() {
		return ctx
	}
	carrier := propagation.MapCarrier{}
	if traceContext.Traceparent != "" {
		carrier.Set("traceparent", traceContext.Traceparent)
	}
	if traceContext.Tracestate != "" {
		carrier.Set("tracestate", traceContext.Tracestate)
	}
	extracted := propagation.TraceContext{}.Extract(ctx, carrier)
	if !trace.SpanFromContext(extracted).SpanContext().IsValid() {
		return ctx
	}
	return extracted
}
