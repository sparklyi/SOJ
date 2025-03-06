package producer

import (
	"context"
	"encoding/json"
	"github.com/streadway/amqp"
	"go.uber.org/zap"
)

type Email struct {
	log          *zap.Logger
	Channel      *amqp.Channel
	ExchangeName string
	QueueName    string
}

// EmailContent 邮件内容 包含收件人 内容 是否延迟发送
type EmailContent struct {
	Target  []string `json:"target"`
	Subject string   `json:"subject"`
	Content string   `json:"content"`
	Code    string   `json:"code"`
}

func NewEmailProducer(log *zap.Logger, conn *amqp.Connection) *Email {
	exchangeName := "exchange_email"
	QueueName := "queue_email"
	ch, err := conn.Channel()
	if err != nil {
		panic("rabbitmq信道创建失败" + err.Error())
	}
	delay, err := ch.QueueDeclare(QueueName, true, false, false, false, nil)
	if err != nil {
		panic("rabbitmq队列创建失败" + err.Error())
	}
	err = ch.ExchangeDeclare(
		exchangeName,
		"x-delayed-message",
		true, false, false, false,
		amqp.Table{
			"x-delayed-type": "direct",
		},
	)
	if err != nil {
		panic("rabbitmq交换机创建失败" + err.Error())
	}
	err = ch.QueueBind(delay.Name, "", exchangeName, false, nil)
	if err != nil {
		panic("rabbitmq交换机绑定失败" + err.Error())
	}
	return &Email{
		log:          log,
		Channel:      ch,
		ExchangeName: exchangeName,
		QueueName:    QueueName,
	}

}

func (p *Email) Send(ctx context.Context, content EmailContent, DelaySeconds int64) error {
	c, err := json.Marshal(content)
	if err != nil {
		p.log.Error("json序列化失败", zap.Error(err))
		return err
	}

	err = p.Channel.Publish(
		p.ExchangeName,
		"",
		false, false,
		amqp.Publishing{

			ContentType: "application/json",
			Body:        c,
			Headers: amqp.Table{
				"x-delay": DelaySeconds * 1000,
			},
		},
	)
	if err != nil {
		p.log.Error("消息发布失败", zap.Error(err))
		return err
	}
	p.log.Info("消息发布成功", zap.Any("content", content))
	return nil
}
