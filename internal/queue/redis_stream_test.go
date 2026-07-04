package queue

import (
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestRedisMessageMappingRejectsInvalidTaskID(t *testing.T) {
	_, err := xMessage(redis.XMessage{ID: "1-0", Values: map[string]any{"task_id": "bad"}})
	if err == nil {
		t.Fatal("expected invalid task id error")
	}
}

func TestRedisMessageMappingPreservesPayloadAndAttempts(t *testing.T) {
	msg, err := xMessage(redis.XMessage{
		ID: "1-0",
		Values: map[string]any{
			"task_id":  "42",
			"payload":  `{"submission_id":7}`,
			"attempts": "2",
		},
	})
	if err != nil {
		t.Fatalf("xMessage returned error: %v", err)
	}
	if msg.TaskID != 42 || string(msg.Payload) != `{"submission_id":7}` || msg.Attempts != 2 {
		t.Fatalf("message = %+v", msg)
	}
}
