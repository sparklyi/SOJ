package submission

import (
	"context"
	"time"

	"SOJ/internal/queue"
)

type taskPublisher interface {
	Publish(context.Context, int64, []byte) (string, error)
}

// MessageAcker acknowledges a processed queue message.
//
// It is exported because the app package's judge-agent loop consumes this
// submission-owned callback contract.
type MessageAcker interface {
	Ack(context.Context, string) error
}

type deadLetterQueue interface {
	MessageAcker
	DeadLetter(context.Context, queue.Message, string) error
}

type taskQueueConsumer interface {
	Ensure(context.Context) error
	Consume(context.Context, int, time.Duration) ([]queue.Message, error)
}

type staleTaskClaimer interface {
	ClaimStale(context.Context, time.Duration, int) ([]queue.Message, error)
}
