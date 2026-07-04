package queue

import (
	"context"
	"time"
)

type Message struct {
	ID       string
	TaskID   int64
	Payload  []byte
	Attempts int
}

type TaskQueue interface {
	Ensure(ctx context.Context) error
	Publish(ctx context.Context, taskID int64, payload []byte) (streamID string, err error)
	Consume(ctx context.Context, limit int, block time.Duration) ([]Message, error)
	ClaimStale(ctx context.Context, minIdle time.Duration, limit int) ([]Message, error)
	Ack(ctx context.Context, messageID string) error
	DeadLetter(ctx context.Context, message Message, reason string) error
	Close() error
}
