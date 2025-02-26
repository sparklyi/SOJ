package initialize

import (
	"SOJ/internal/mq"
	"SOJ/internal/mq/consumer"
	"github.com/spf13/viper"
	"github.com/streadway/amqp"
)

func InitRabbitMQ() *amqp.Connection {
	conn, err := amqp.Dial(viper.GetString("rabbitmq.url"))
	if err != nil {
		panic("rabbitmq连接失败")
		return nil
	}
	return conn
}

func InitConsumer(
	contest *consumer.ContestConsumer,
	email *consumer.EmailConsumer,
) []mq.Consumer {
	return []mq.Consumer{contest, email}
}
