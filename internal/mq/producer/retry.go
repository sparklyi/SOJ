package producer

import (
	"context"
	"encoding/json"
	"github.com/streadway/amqp"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

type Retry struct {
	log          *zap.Logger
	Channel      *amqp.Channel
	ExchangeName string
	QueueName    string
	DLXExchange  string // 新增死信交换机
	DLXQueue     string // 新增死信队列
}

func NewRetryProducer(log *zap.Logger, conn *amqp.Connection) *Retry {
	exchangeName := "exchange_retry"
	QueueName := "queue_retry"
	dlxExchange := "exchange_retry_dlx"
	dlxQueue := "queue_retry_dlx"

	ch, err := conn.Channel()
	if err != nil {
		panic("rabbitmq信道创建失败")
	}
	//创建死信队列
	createDLX(ch, dlxExchange, dlxQueue)

	//声明队列
	queue, err := ch.QueueDeclare(
		QueueName,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-dead-letter-exchange": dlxExchange,         // 死信交换机
			"x-message-ttl":          24 * 60 * 60 * 1000, // 消息存活时间
			"x-max-length":           1000,                // 队列最大长度
		})
	if err != nil {
		panic("rabbitmq队列创建失败")
	}
	//声明交换机
	err = ch.ExchangeDeclare(exchangeName, "direct", true, false, false, false, nil)
	if err != nil {
		panic("rabbitmq交换机创建失败" + err.Error())
	}
	//队列绑定交换机
	err = ch.QueueBind(queue.Name, "", exchangeName, false, nil)
	if err != nil {
		panic("rabbitmq交换机绑定失败" + err.Error())
	}
	return &Retry{
		log:          log,
		Channel:      ch,
		ExchangeName: exchangeName,
		QueueName:    queue.Name,
		DLXExchange:  dlxExchange,
		DLXQueue:     dlxQueue,
	}
}

func createDLX(ch *amqp.Channel, DLXExchange, DLXQueue string) {
	// 声明死信交换机
	err := ch.ExchangeDeclare(
		DLXExchange,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		panic("死信交换机创建失败: " + err.Error())
	}

	// 声明死信队列
	_, err = ch.QueueDeclare(
		DLXQueue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		panic("死信队列创建失败: " + err.Error())
	}

	// 绑定死信队列到死信交换机
	err = ch.QueueBind(
		DLXQueue,
		"", // routing key
		DLXExchange,
		false,
		nil,
	)
	if err != nil {
		panic("死信队列绑定失败: " + err.Error())
	}

}

type RetryContent struct {
	FuncName string             `json:"func_name,omitempty"`
	ObjectID primitive.ObjectID `json:"object_id,omitempty"`
	Params   string             `json:"params,omitempty"`
}

func (r *Retry) Send(ctx context.Context, req RetryContent) {
	j, err := json.Marshal(req)
	if err != nil {
		r.log.Error("json序列化失败", zap.Error(err))
		return
	}
	err = r.Channel.Publish(
		r.ExchangeName,
		"",
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        j,
		},
	)
	if err != nil {
		r.log.Error("消息发布失败", zap.Error(err))
		return
	}
	r.log.Info("消息发布成功", zap.Any("content", j))

}
