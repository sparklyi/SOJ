package mq

import "context"

type Consumer interface {
	Consume(ctx context.Context)
}

var ReTryDelaySeconds = []int{0, 5, 10, 30, 60} //最大重试消费次数及延迟
