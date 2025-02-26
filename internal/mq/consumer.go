package mq

import "context"

type Consumer interface {
	Consume(ctx context.Context)
}

const (
	MaxRetry = 5 //最大重试消费次数
)
