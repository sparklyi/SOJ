package submission

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"SOJ/internal/queue"
)

type retryWriteStoreRecorder struct {
	calls     *[]string
	retryErr  error
	queuedErr error
}

func (s *retryWriteStoreRecorder) RetryJudgeTask(
	_ context.Context,
	_ int64,
	_ time.Time,
	_ string,
) (JudgeTaskRecord, error) {
	*s.calls = append(*s.calls, "retry")
	return JudgeTaskRecord{}, s.retryErr
}

func (s *retryWriteStoreRecorder) MarkSubmissionQueued(
	_ context.Context,
	_ int64,
) (SubmissionRecord, error) {
	*s.calls = append(*s.calls, "queued")
	return SubmissionRecord{}, s.queuedErr
}

func (s *retryWriteStoreRecorder) MarkJudgeTaskDead(
	_ context.Context,
	_ int64,
	_ string,
) (JudgeTaskRecord, error) {
	*s.calls = append(*s.calls, "dead")
	return JudgeTaskRecord{}, nil
}

type retryWriteQueueRecorder struct {
	calls *[]string
}

func (q *retryWriteQueueRecorder) Ack(
	_ context.Context,
	messageID string,
) error {
	*q.calls = append(*q.calls, "ack:"+messageID)
	return nil
}

func (q *retryWriteQueueRecorder) DeadLetter(
	_ context.Context,
	_ queue.Message,
	_ string,
) error {
	*q.calls = append(*q.calls, "dead-letter")
	return nil
}

func TestTaskFailureHandlerRetryWriteOrder(t *testing.T) {
	retryErr := errors.New("retry write failed")
	queuedErr := errors.New("submission status write failed")

	tests := []struct {
		name       string
		retryErr   error
		queuedErr  error
		wantResult string
		wantErr    error
		wantCalls  []string
	}{
		{
			name:       "successful retry",
			wantResult: "retry",
			wantCalls:  []string{"retry", "queued", "ack:1-0"},
		},
		{
			name:       "retry write failure",
			retryErr:   retryErr,
			wantResult: "error",
			wantErr:    retryErr,
			wantCalls:  []string{"retry"},
		},
		{
			name:       "submission status write failure",
			queuedErr:  queuedErr,
			wantResult: "error",
			wantErr:    queuedErr,
			wantCalls:  []string{"retry", "queued"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string

			store := &retryWriteStoreRecorder{
				calls:     &calls,
				retryErr:  tt.retryErr,
				queuedErr: tt.queuedErr,
			}

			taskQueue := &retryWriteQueueRecorder{
				calls: &calls,
			}

			handler := NewTaskFailureHandler(
				store, taskQueue, 3, nil, nil,
			)

			submissionID := int64(2)

			message := queue.Message{
				ID:     "1-0",
				TaskID: 1,
			}

			task := JudgeTaskRecord{
				ID:           1,
				SubmissionID: &submissionID,
				Attempts:     1,
			}

			result, err := handler.retryOrDead(
				t.Context(),
				message,
				task,
				errors.New("judge unavailable"),
			)

			if err != tt.wantErr {
				t.Errorf(
					"retryOrDead() error = %v, want %v",
					err, tt.wantErr,
				)
			}

			if result != tt.wantResult {
				t.Errorf(
					"retryOrDead() result = %q, want %q",
					result, tt.wantResult,
				)
			}

			if !slices.Equal(calls, tt.wantCalls) {
				t.Errorf(
					"call order = %v, want %v",
					calls, tt.wantCalls,
				)
			}
		})
	}
}
