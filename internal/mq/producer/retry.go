package producer

import (
	"github.com/streadway/amqp"
	"go.uber.org/zap"
)

type Retry struct {
	log          *zap.Logger
	Channel      *amqp.Channel
	ExchangeName string
	QueueName    string
}

//func NewRetryProducer(log *zap.Logger, conn *amqp.Connection) *Retry {
//	exchangeName := "exchange_email"
//	QueueName := "queue_email"
//	ch, err := conn.Channel()
//	if err != nil {
//		panic("rabbitmq信道创建失败")
//	}
//	//声明队列
//	queue, err := ch.QueueDeclare(QueueName, true, false, false, false, nil)
//	if err != nil {
//		panic("rabbitmq队列创建失败")
//	}
//	//声明交换机
//	err = ch.ExchangeDeclare(exchangeName, "direct", true, false, false, false, nil)
//
//}
