package mq

import (
	"SOJ/pkg/email"
	"context"
	"encoding/json"
	"go.uber.org/zap"
)

type EmailConsumer struct {
	log   *zap.Logger
	email *email.Email
	*EmailProducer
}

func NewEmailConsumer(log *zap.Logger, email *email.Email, p *EmailProducer) *EmailConsumer {
	return &EmailConsumer{
		log:           log,
		email:         email,
		EmailProducer: p,
	}

}

func (c *EmailConsumer) Consume(ctx context.Context) error {
	c.log.Info("start consume email")
	defer c.log.Info("end consume email")

	//从信道中消费消息
	msgs, err := c.Channel.Consume(c.QueueName, "", false, false, false, false, nil)
	if err != nil {
		c.log.Error("consume email fail", zap.Error(err))
		return err
	}

	for msg := range msgs {
		content := EmailContent{}
		err = json.Unmarshal(msg.Body, &content)
		if err != nil {
			c.log.Error("unmarshal email fail", zap.Error(err))
			msg.Nack(false, false)
			continue
		}
		err = c.email.Send(content.Target, content.Content)
		if err != nil {
			c.log.Error("send email fail", zap.Error(err))
			msg.Nack(false, true)
		} else {
			msg.Ack(true)
		}
	}
	return nil
}
