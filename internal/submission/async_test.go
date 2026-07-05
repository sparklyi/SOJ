package submission

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"SOJ/internal/judge"
	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/queue"
)

func TestDispatcherPublishesRequestEventWithoutCallingJudgeEngine(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	repo.tasks[7] = JudgeTaskRecord{ID: 7, SubmissionID: 9, Status: "pending"}
	repo.submissions[9] = SubmissionRecord{ID: 9, ProblemID: 1, LanguageID: 71, SourceArtifactID: 4, Status: StatusQueued, TestcaseSetID: 3}
	repo.artifacts[4] = ArtifactRecord{ID: 4, StorageKey: "source/key", ChecksumSHA256: "sha256:source", SizeBytes: 12}
	repo.languages[71] = LanguageRecord{ID: 71, DefaultTimeLimit: time.Second, DefaultMemoryKB: 262144, Enabled: true}
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
	if event.AttemptID == "" || event.EventID == "" || event.TraceID == "" {
		t.Fatalf("event identity = %+v", event)
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

type recordingResultPublisher struct {
	eventsPayloads [][]byte
	events         *[]string
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
