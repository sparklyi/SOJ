package submission

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"SOJ/internal/judge"
	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/queue"
)

type resultConsumerStoreStub struct {
	input CompleteJudgeAttemptResultInput
}

func (s *resultConsumerStoreStub) CompleteJudgeAttemptResult(_ context.Context, input CompleteJudgeAttemptResultInput) (bool, error) {
	s.input = input
	return true, nil
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

type recordingResultStore struct {
	calls *[]string
	err   error
}

func (s recordingResultStore) CompleteJudgeAttemptResult(context.Context, CompleteJudgeAttemptResultInput) (bool, error) {
	*s.calls = append(*s.calls, "persist")
	return s.err == nil, s.err
}

type recordingMessageAcker struct {
	calls *[]string
	acked []string
	err   error
}

func (a *recordingMessageAcker) Ack(_ context.Context, id string) error {
	*a.calls = append(*a.calls, "ack")
	a.acked = append(a.acked, id)
	return a.err
}

func TestResultConsumerFailureBoundaries(t *testing.T) {
	persistErr := errors.New("persist failed")
	ackErr := errors.New("ack failed")
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

	tests := []struct {
		name      string
		storeErr  error
		ackErr    error
		wantErr   error
		wantCalls []string
		wantAcked []string
	}{
		{name: "persistence failure skips ack", storeErr: persistErr, wantErr: persistErr, wantCalls: []string{"persist"}},
		{name: "ack failure after persistence", ackErr: ackErr, wantErr: ackErr, wantCalls: []string{"persist", "ack"}, wantAcked: []string{"7-0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			acker := &recordingMessageAcker{calls: &calls, err: tt.ackErr}
			consumer := NewResultConsumer(recordingResultStore{calls: &calls, err: tt.storeErr})

			err := consumer.ProcessResultMessage(t.Context(), queue.Message{ID: "7-0", Payload: payload}, acker)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(calls, tt.wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, tt.wantCalls)
			}
			if !slices.Equal(acker.acked, tt.wantAcked) {
				t.Fatalf("acked = %v, want %v", acker.acked, tt.wantAcked)
			}
		})
	}
}
