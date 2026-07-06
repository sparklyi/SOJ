package submission

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"SOJ/internal/judge"
	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/judgecore"
	"SOJ/internal/judgecore/language"
	"SOJ/internal/queue"

	"go.opentelemetry.io/otel/trace"
)

func TestDispatcherPublishesRequestEventWithoutCallingJudgeEngine(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	repo.tasks[7] = JudgeTaskRecord{ID: 7, SubmissionID: 9, Status: "pending"}
	repo.submissions[9] = SubmissionRecord{ID: 9, ProblemID: 1, LanguageID: 71, SourceArtifactID: 4, Status: StatusQueued, TestcaseSetID: 3}
	repo.artifacts[4] = ArtifactRecord{ID: 4, StorageKey: "source/key", ChecksumSHA256: "sha256:source", SizeBytes: 12}
	repo.languages[71] = LanguageRecord{ID: 71, EngineLanguageID: "go", DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144, Enabled: true}
	engine := judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictAccepted})
	q := &memoryQueue{}
	worker := NewWorker(WorkerOptions{
		Repository:       repo,
		Queue:            q,
		Judge:            engine,
		ProblemReader:    fakeProblemReader{},
		TestcaseResolver: fakeTestcaseResolver{},
		SourceStore:      NewMemorySourceStore(),
		Now:              func() time.Time { return time.Unix(100, 0).UTC() },
	})

	count, err := worker.DispatchPending(ctx, 1)
	if err != nil {
		t.Fatalf("DispatchPending returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if len(engine.Requests()) != 0 {
		t.Fatalf("judge engine was called: %+v", engine.Requests())
	}
	if len(q.payloads) != 1 {
		t.Fatalf("published payloads = %d", len(q.payloads))
	}
	var event judgeevents.RequestEvent
	if err := json.Unmarshal(q.payloads[0], &event); err != nil {
		t.Fatalf("decode request event: %v", err)
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("request event validation failed: %v", err)
	}
	if event.SourceArtifact.ID != 4 || event.SourceArtifact.StorageKey != "source/key" || event.SourceArtifact.ContentHash != "sha256:source" {
		t.Fatalf("source artifact = %+v", event.SourceArtifact)
	}
	if event.LanguageSlug != "go" {
		t.Fatalf("language_slug = %q, want go", event.LanguageSlug)
	}
	if event.AttemptID == "" || event.EventID == "" || event.TraceID == "" {
		t.Fatalf("event identity = %+v", event)
	}
	if event.TraceID != "trace-submission-9-task-7" {
		t.Fatalf("trace_id = %q, want disabled fallback", event.TraceID)
	}
	if event.TraceContext != (judgeevents.TraceContext{}) {
		t.Fatalf("trace_context = %+v, want empty when tracing is disabled", event.TraceContext)
	}
	var raw map[string]any
	if err := json.Unmarshal(q.payloads[0], &raw); err != nil {
		t.Fatalf("decode raw request event: %v", err)
	}
	if _, ok := raw["trace_context"]; ok {
		t.Fatalf("trace_context serialized while tracing is disabled: %s", q.payloads[0])
	}
}

func TestDispatcherPublishesW3CTraceContextFromActiveSpan(t *testing.T) {
	traceID := trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36}
	spanID := trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	}))
	repo := newMemoryRepo()
	repo.tasks[7] = JudgeTaskRecord{ID: 7, SubmissionID: 9, Status: "pending"}
	repo.submissions[9] = SubmissionRecord{ID: 9, ProblemID: 1, LanguageID: 71, SourceArtifactID: 4, Status: StatusQueued, TestcaseSetID: 3}
	repo.artifacts[4] = ArtifactRecord{ID: 4, StorageKey: "source/key", ChecksumSHA256: "sha256:source", SizeBytes: 12}
	repo.languages[71] = LanguageRecord{ID: 71, EngineLanguageID: "go", DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144, Enabled: true}
	q := &memoryQueue{}
	worker := NewWorker(WorkerOptions{
		Repository:       repo,
		Queue:            q,
		Judge:            judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictAccepted}),
		ProblemReader:    fakeProblemReader{},
		TestcaseResolver: fakeTestcaseResolver{},
		SourceStore:      NewMemorySourceStore(),
		Now:              func() time.Time { return time.Unix(100, 0).UTC() },
	})

	if _, err := worker.DispatchPending(ctx, 1); err != nil {
		t.Fatalf("DispatchPending returned error: %v", err)
	}
	var event judgeevents.RequestEvent
	if err := json.Unmarshal(q.payloads[0], &event); err != nil {
		t.Fatalf("decode request event: %v", err)
	}
	if event.TraceID != traceID.String() {
		t.Fatalf("trace_id = %q, want OTel trace ID %q", event.TraceID, traceID.String())
	}
	if event.TraceContext.Traceparent != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("traceparent = %q", event.TraceContext.Traceparent)
	}
	attempt, err := repo.GetLatestJudgeAttemptBySubmissionID(context.Background(), 9)
	if err != nil {
		t.Fatalf("GetLatestJudgeAttemptBySubmissionID returned error: %v", err)
	}
	if attempt.TraceID == nil || *attempt.TraceID != traceID.String() {
		t.Fatalf("attempt trace_id = %v, want %s", attempt.TraceID, traceID.String())
	}
}

func TestFakeAsyncAgentPublishesResultBeforeRequestAck(t *testing.T) {
	ctx := context.Background()
	source := []byte("package main")
	store := NewMemorySourceStore()
	store.objects["source/key"] = source
	request := judgeevents.RequestEvent{
		EventID:        "evt-request-1",
		AttemptID:      "attempt-1",
		TraceID:        "trace-1",
		SubmissionID:   9,
		LanguageID:     71,
		SourceArtifact: judgeevents.ArtifactRef{ID: 4, StorageKey: "source/key", ContentHash: "sha256:source"},
		TestcaseSet:    judgeevents.TestcaseSetRef{ID: 3, Hash: "cases-hash"},
		TimeoutMS:      1000,
		CreatedAt:      time.Unix(100, 0).UTC(),
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resultQueue := &recordingResultPublisher{}
	agent := NewFakeAsyncAgent(FakeAsyncAgentOptions{
		Judge:           judge.NewFakeEngine(judge.Result{Verdict: judge.VerdictMemoryLimitExceeded, JudgedAt: time.Unix(101, 0).UTC()}),
		SourceStore:     store,
		ResultPublisher: resultQueue,
	})
	requestQueue := &memoryQueue{events: &[]string{}}
	events := []string{}
	requestQueue.events = &events
	resultQueue.events = &events

	if err := agent.ProcessRequestMessage(ctx, queue.Message{ID: "1-0", TaskID: 7, Payload: payload}, requestQueue); err != nil {
		t.Fatalf("ProcessRequestMessage returned error: %v", err)
	}
	if len(events) != 2 || events[0] != "publish_result" || events[1] != "ack" {
		t.Fatalf("events = %v", events)
	}
	if len(resultQueue.eventsPayloads) != 1 {
		t.Fatalf("result payload count = %d", len(resultQueue.eventsPayloads))
	}
	var result judgeevents.ResultEvent
	if err := json.Unmarshal(resultQueue.eventsPayloads[0], &result); err != nil {
		t.Fatalf("decode result event: %v", err)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
	if result.RequestEventID != request.EventID || result.AttemptID != request.AttemptID || result.Status != judge.VerdictMemoryLimit {
		t.Fatalf("result = %+v", result)
	}
}

func TestFakeAsyncAgentPropagatesRequestTraceContextToJudgeAndResult(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySourceStore()
	store.objects["source/key"] = []byte("package main")
	request := judgeevents.RequestEvent{
		EventID:      "evt-request-1",
		AttemptID:    "attempt-1",
		TraceID:      "4bf92f3577b34da6a3ce929d0e0e4736",
		TraceContext: judgeevents.TraceContext{Traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		SubmissionID: 9,
		LanguageID:   71,
		SourceArtifact: judgeevents.ArtifactRef{
			ID:          4,
			StorageKey:  "source/key",
			ContentHash: "sha256:source",
		},
		TestcaseSet: judgeevents.TestcaseSetRef{ID: 3, Hash: "cases-hash"},
		TimeoutMS:   1000,
		CreatedAt:   time.Unix(100, 0).UTC(),
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	engine := &contextCapturingEngine{result: judge.Result{Verdict: judge.VerdictAccepted, JudgedAt: time.Unix(101, 0).UTC()}}
	resultQueue := &recordingResultPublisher{}
	agent := NewFakeAsyncAgent(FakeAsyncAgentOptions{
		Judge:           engine,
		SourceStore:     store,
		ResultPublisher: resultQueue,
	})

	if err := agent.ProcessRequestMessage(ctx, queue.Message{ID: "1-0", TaskID: 7, Payload: payload}, &memoryQueue{}); err != nil {
		t.Fatalf("ProcessRequestMessage returned error: %v", err)
	}
	if !engine.spanContext.IsValid() || engine.spanContext.TraceID().String() != request.TraceID {
		t.Fatalf("judge span context = %s, want request trace", engine.spanContext.TraceID().String())
	}
	var result judgeevents.ResultEvent
	if err := json.Unmarshal(resultQueue.eventsPayloads[0], &result); err != nil {
		t.Fatalf("decode result event: %v", err)
	}
	if result.TraceID != request.TraceID || result.TraceContext != request.TraceContext {
		t.Fatalf("result trace fields = trace_id %q context %+v", result.TraceID, result.TraceContext)
	}
}

func TestCoreAsyncAgentRunsJudgeCoreCasesAndPublishesResult(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySourceStore()
	store.objects["source/key"] = []byte(`package main
import "fmt"
func main() { var a, b int; fmt.Scan(&a, &b); fmt.Println(a + b) }
`)
	request := judgeevents.RequestEvent{
		EventID:        "evt-request-core",
		AttemptID:      "attempt-core",
		TraceID:        "trace-core",
		SubmissionID:   9,
		LanguageID:     language.GoID,
		SourceArtifact: judgeevents.ArtifactRef{ID: 4, StorageKey: "source/key", ContentHash: "sha256:source"},
		TestcaseSet:    judgeevents.TestcaseSetRef{ID: 3, Hash: "cases-hash"},
		Testcases: []judgeevents.TestcaseRef{{
			Index:             1,
			InputKey:          "1 2\n",
			ExpectedOutputKey: "3\n",
			TimeLimitMS:       1000,
			MemoryKB:          262144,
		}},
		TimeoutMS: 1000,
		MemoryKB:  262144,
		CreatedAt: time.Unix(100, 0).UTC(),
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resultQueue := &recordingResultPublisher{}
	agent := NewCoreAsyncAgent(CoreAsyncAgentOptions{
		Core:            judgecore.New(judgecore.Options{}),
		SourceStore:     store,
		ResultPublisher: resultQueue,
		Now:             func() time.Time { return time.Unix(101, 0).UTC() },
	})
	requestQueue := &memoryQueue{events: &[]string{}}
	events := []string{}
	requestQueue.events = &events
	resultQueue.events = &events

	if err := agent.ProcessRequestMessage(ctx, queue.Message{ID: "1-0", TaskID: 7, Payload: payload}, requestQueue); err != nil {
		t.Fatalf("ProcessRequestMessage returned error: %v", err)
	}
	if len(events) != 2 || events[0] != "publish_result" || events[1] != "ack" {
		t.Fatalf("events = %v", events)
	}
	if len(resultQueue.eventsPayloads) != 1 {
		t.Fatalf("result payload count = %d", len(resultQueue.eventsPayloads))
	}
	var result judgeevents.ResultEvent
	if err := json.Unmarshal(resultQueue.eventsPayloads[0], &result); err != nil {
		t.Fatalf("decode result event: %v", err)
	}
	if result.Status != judge.VerdictAccepted || result.Result.Verdict != judge.VerdictAccepted {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Result.Cases) != 1 || result.Result.Cases[0].Verdict != judge.VerdictAccepted {
		t.Fatalf("cases = %+v", result.Result.Cases)
	}
	if result.Result.Cases[0].TestcaseKey != "testcase-set-3/case-1" {
		t.Fatalf("testcase key = %q", result.Result.Cases[0].TestcaseKey)
	}
	if result.Result.Manifest.TestcaseSetHash != "cases-hash" || result.Result.Manifest.TraceID != "trace-core" {
		t.Fatalf("manifest = %+v", result.Result.Manifest)
	}
}

func TestResultConsumerIsIdempotentAndAcksAfterPersist(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	repo.submissions[9] = SubmissionRecord{ID: 9, ProblemID: 1, LanguageID: 71, SourceArtifactID: 4, Status: StatusRunning, TestcaseSetID: 3}
	attempt, err := repo.EnsureJudgeAttempt(ctx, EnsureJudgeAttemptInput{
		AttemptID:       "attempt-1",
		SubmissionID:    9,
		TaskID:          7,
		LanguageID:      71,
		ProtocolVersion: judgeevents.RequestEventType,
		JudgeEngine:     judge.EngineSOJAgent,
	})
	if err != nil {
		t.Fatalf("EnsureJudgeAttempt returned error: %v", err)
	}
	event := judgeevents.ResultEvent{
		EventID:        "evt-result-1",
		RequestEventID: "evt-request-1",
		AttemptID:      strconv.FormatInt(attempt.ID, 10),
		TraceID:        "trace-1",
		Status:         judge.VerdictTimeLimitExceeded,
		Result:         judge.Result{Verdict: judge.VerdictTimeLimitExceeded, JudgedAt: time.Unix(101, 0).UTC()},
		JudgedAt:       time.Unix(101, 0).UTC(),
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	q := &memoryQueue{events: &repo.events}
	consumer := NewResultConsumer(ResultConsumerOptions{Repository: repo})

	if err := consumer.ProcessResultMessage(ctx, queue.Message{ID: "2-0", TaskID: 7, Payload: payload}, q); err != nil {
		t.Fatalf("first ProcessResultMessage returned error: %v", err)
	}
	if repo.submissionUpdates != 1 || len(q.acked) != 1 {
		t.Fatalf("updates=%d acked=%v", repo.submissionUpdates, q.acked)
	}
	if repo.events[len(repo.events)-1] != "ack" {
		t.Fatalf("events = %v", repo.events)
	}
	if repo.results[9].AttemptID != attempt.ID || repo.submissions[9].Status != StatusTimeLimit {
		t.Fatalf("result projection=%+v submission=%+v", repo.results[9], repo.submissions[9])
	}

	if err := consumer.ProcessResultMessage(ctx, queue.Message{ID: "3-0", TaskID: 7, Payload: payload}, q); err != nil {
		t.Fatalf("duplicate ProcessResultMessage returned error: %v", err)
	}
	if repo.submissionUpdates != 1 || len(q.acked) != 2 {
		t.Fatalf("after duplicate updates=%d acked=%v", repo.submissionUpdates, q.acked)
	}
}

func TestRecoverDeadJudgeTaskResetsRetryBudgetAndQueuesSubmission(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	repo.tasks[7] = JudgeTaskRecord{
		ID:           7,
		SubmissionID: 9,
		Status:       "dead",
		Attempts:     5,
		LastError:    "runner image missing",
	}
	repo.submissions[9] = SubmissionRecord{ID: 9, Status: StatusSystemErr, ErrorMessage: stringPtr("runner image missing")}
	nextRunAt := time.Unix(200, 0).UTC()

	task, err := repo.RecoverDeadJudgeTask(ctx, 7, nextRunAt, "manual recovery after runner fix")
	if err != nil {
		t.Fatalf("RecoverDeadJudgeTask returned error: %v", err)
	}
	if task.Status != "pending" || task.Attempts != 0 || !task.NextRunAt.Equal(nextRunAt) {
		t.Fatalf("task after recovery = %+v", task)
	}
	if task.LastError != "manual recovery after runner fix" {
		t.Fatalf("last error = %q", task.LastError)
	}
	submission := repo.submissions[9]
	if submission.Status != StatusQueued {
		t.Fatalf("submission status = %q, want queued", submission.Status)
	}
	if submission.ErrorMessage == nil || *submission.ErrorMessage != "manual recovery after runner fix" {
		t.Fatalf("submission error = %v", submission.ErrorMessage)
	}
}

type recordingResultPublisher struct {
	eventsPayloads [][]byte
	events         *[]string
}

type contextCapturingEngine struct {
	result      judge.Result
	spanContext trace.SpanContext
}

func (e *contextCapturingEngine) Judge(ctx context.Context, request judge.Request) (judge.Result, error) {
	e.spanContext = trace.SpanFromContext(ctx).SpanContext()
	return e.result, nil
}

func (e *contextCapturingEngine) Languages(ctx context.Context) ([]judge.Language, error) {
	return nil, nil
}

func (p *recordingResultPublisher) PublishResult(ctx context.Context, event judgeevents.ResultEvent) (string, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	p.eventsPayloads = append(p.eventsPayloads, payload)
	if p.events != nil {
		*p.events = append(*p.events, "publish_result")
	}
	return "result-1-0", nil
}
