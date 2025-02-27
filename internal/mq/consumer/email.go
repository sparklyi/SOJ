package consumer

import (
	"SOJ/internal/mq"
	"SOJ/internal/mq/producer"
	"SOJ/pkg/email"
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"github.com/streadway/amqp"
	"go.uber.org/zap"
	"time"
)

type EmailConsumer struct {
	log   *zap.Logger
	email *email.Email
	*producer.Email
	rs *redis.Client
}

// NewEmailConsumer 依赖注入方法
func NewEmailConsumer(log *zap.Logger, email *email.Email, p *producer.Email, rs *redis.Client) *EmailConsumer {
	return &EmailConsumer{
		log:   log,
		email: email,
		Email: p,
		rs:    rs,
	}

}

// Consume 消费队列消息
func (c *EmailConsumer) Consume(ctx context.Context) {
	c.log.Info("start consume email")
	defer c.log.Info("end consume email")

	//从信道中消费消息
	msgs, err := c.Channel.Consume(c.QueueName, "", true, false, false, false, nil)
	if err != nil {
		c.log.Error("consume email fail", zap.Error(err))
		return
	}
	//协程池
	workerPool := make(chan struct{}, 128)
	for msg := range msgs {
		//获取协程
		workerPool <- struct{}{}
		//开启协程
		go func(msg amqp.Delivery) {
			//释放
			defer func() { <-workerPool }()
			content := producer.EmailContent{}
			err = json.Unmarshal(msg.Body, &content)
			if err != nil {
				c.log.Error("unmarshal email fail", zap.Error(err))
				return
			}
			if len(content.Target) == 0 {
				c.log.Error("邮件接收方为空", zap.Any("content", content))
				return
			}

			for _, second := range mq.ReTryDelaySeconds {
				time.Sleep(time.Duration(second) * time.Second)
				//如果发送的是验证码
				if content.Code != "" {
					if err = c.rs.Set(ctx, content.Target[0], content.Code, time.Minute).Err(); err != nil {
						c.log.Error("验证码缓存失败", zap.Error(err))
						continue
					}
				}
				if err = c.email.Send(content.Target, content.Subject, content.Content); err != nil {
					continue
				}
				//执行到这则已经完成缓存+发送
				return
			}
			//执行到这 即重试maxRetry次依然失败，直接丢弃并记日志
			c.log.Error("消费达到最大次数", zap.Any("content", content))
		}(msg)
	}
}
