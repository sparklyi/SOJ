package producer

import (
	"context"
	"encoding/json"
	"github.com/spf13/viper"
	"github.com/streadway/amqp"
	"go.uber.org/zap"
)

type Contest struct {
	log          *zap.Logger
	Channel      *amqp.Channel
	ExchangeName string
	QueueName    string
}

func NewContestProducer(log *zap.Logger, conn *amqp.Connection) *Contest {
	exchangeName := viper.GetString("rabbitmq.exchange_contest")
	QueueName := viper.GetString("rabbitmq.queue_contest")
	ch, err := conn.Channel()
	if err != nil {
		panic("rabbitmq信道创建失败")
		return nil
	}
	delay, err := ch.QueueDeclare(QueueName, true, false, false, false, nil)
	if err != nil {
		panic("rabbitmq队列创建失败")
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
		panic("rabbitmq交换机创建失败")
		return nil
	}
	err = ch.QueueBind(delay.Name, "", exchangeName, false, nil)
	if err != nil {
		panic("rabbitmq交换机绑定失败")
		return nil
	}
	return &Contest{
		log:          log,
		Channel:      ch,
		ExchangeName: exchangeName,
		QueueName:    QueueName,
	}

}

type ContestNotify struct {
	ContestID uint   `json:"contest_id"`
	Subject   string `json:"subject"`
	Content   string `json:"content"`
}

func (c *Contest) Producer(ctx context.Context, req *ContestNotify, delay int64) error {
	content, err := json.Marshal(req)
	if err != nil {
		c.log.Error("json序列化失败", zap.Error(err))
		return err
	}

	err = c.Channel.Publish(
		c.ExchangeName,
		"",
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
