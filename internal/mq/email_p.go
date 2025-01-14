package mq

import (
	"context"
	"encoding/json"
	"github.com/spf13/viper"
	"github.com/streadway/amqp"
	"go.uber.org/zap"
)

type EmailProducer struct {
	log      *zap.Logger
	Conn     *amqp.Connection
	Channel  *amqp.Channel
	Exchange string
	Queue    string
}

// EmailContent 邮件内容 包含收件人 内容 是否延迟发送
type EmailContent struct {
	Target  []string `json:"target"`
	Content string   `json:"content"`
}

func NewEmailProducer(log *zap.Logger) *EmailProducer {
	exchangeName := viper.GetString("rabbitmq.exchange")
	QueueName := viper.GetString("rabbitmq.queue")
	conn, err := amqp.Dial(viper.GetString("rabbitmq.url"))
	if err != nil {
		log.Error("rabbitmq连接失败", zap.Error(err))
		return nil
	}
	ch, err := conn.Channel()
	if err != nil {
		log.Error("rabbitmq信道创建失败", zap.Error(err))
		return nil
	}
	delay, err := ch.QueueDeclare(QueueName, true, false, false, false, nil)
	if err != nil {
		log.Error("rabbitmq队列创建失败", zap.Error(err))
		return nil
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
		log.Error("rabbitmq交换机创建失败", zap.Error(err))
		return nil
	}
	err = ch.QueueBind(delay.Name, "", exchangeName, false, nil)
	if err != nil {
		log.Error("rabbitmq交换机绑定失败", zap.Error(err))
		return nil
	}
	return &EmailProducer{
		log:      log,
		Conn:     conn,
		Channel:  ch,
		Exchange: exchangeName,
		Queue:    QueueName,
	}

}

func (p *EmailProducer) Send(ctx context.Context, content EmailContent, DelaySeconds int64) error {
	c, err := json.Marshal(content)
	if err != nil {
		p.log.Error("json序列化失败", zap.Error(err))
		return err
	}

	err = p.Channel.Publish(
		p.Exchange,
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
	return nil
}
