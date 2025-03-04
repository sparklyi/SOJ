package producer

import (
	"context"
	"encoding/json"
	"github.com/streadway/amqp"
	"go.uber.org/zap"
)

type Contest struct {
	*Email
	QueueName string
}

func NewContestProducer(log *zap.Logger, email *Email) *Contest {

	QueueName := "queue_contest"
	ch := email.Channel
	delay, err := ch.QueueDeclare(QueueName, true, false, false, false, nil)
	if err != nil {
		panic("rabbitmq队列创建失败")
	}
	//绑定到邮件交换机，路由键为contest
	err = ch.QueueBind(delay.Name, "contest", email.ExchangeName, false, nil)
	if err != nil {
		panic("rabbitmq交换机绑定失败" + err.Error())
	}
	return &Contest{
		email,
		QueueName,
	}

}

type ContestNotify struct {
	ContestID uint   `json:"contest_id"`
	Subject   string `json:"subject"`
	Content   string `json:"content"`
}

func (c *Contest) Producer(ctx context.Context, req ContestNotify, delay int64) error {
	content, err := json.Marshal(req)
	if err != nil {
		c.log.Error("json序列化失败", zap.Error(err))
		return err
	}

	err = c.Channel.Publish(
		c.ExchangeName,
		"contest",
		false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        content,
			Headers: amqp.Table{
				"x-delay": delay * 1000,
			},
		},
	)
	if err != nil {
		c.log.Error("比赛消息发布失败", zap.Error(err))
		return err
	}
	c.log.Info("比赛消息发布成功", zap.Any("content", content))
	return nil
}
