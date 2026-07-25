package submission

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"SOJ/internal/judge"
	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/queue"
)

type resultConsumerStoreStub struct {
	input CompleteJudgeAttemptResultInput
}

func (s *resultConsumerStoreStub) CompleteJudgeAttemptResult(_ context.Context, input CompleteJudgeAttemptResultInput) (SubmissionRecord, bool, error) {
	s.input = input
	return SubmissionRecord{ID: 1, Status: StatusAccepted}, true, nil
}

func TestResultConsumerUsesOnlyResultStore(t *testing.T) {
	store := &resultConsumerStoreStub{}
	consumer := NewResultConsumer(store)
	payload, err := json.Marshal(judgeevents.ResultEvent{
		EventID:        "result-1",
		RequestEventID: "request-1",
		AttemptID:      "attempt-1",
		TraceID:        "trace-1",
		Status:         judge.VerdictAccepted,
		Result:         judge.Result{Verdict: judge.VerdictAccepted, JudgedAt: time.Unix(10, 0).UTC()},
		JudgedAt:       time.Unix(10, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("marshal result event: %v", err)
	}
	resultQueue := &messageAckerStub{}

	if err := consumer.ProcessResultMessage(t.Context(), queue.Message{ID: "1-0", Payload: payload}, resultQueue); err != nil {
		t.Fatalf("ProcessResultMessage() error = %v", err)
	}
	if store.input.EventID != "result-1" || len(resultQueue.acked) != 1 {
		t.Fatalf("persisted input = %+v, acked = %v", store.input, resultQueue.acked)
	}
}
