package consumer

import (
	"SOJ/internal/entity"
	"SOJ/internal/mq"
	"SOJ/internal/mq/producer"
	"SOJ/internal/repository"
	"context"
	"encoding/json"
	"github.com/streadway/amqp"
	"go.uber.org/zap"
	"time"
)

type fn func(ctx context.Context, msg amqp.Delivery, detail producer.RetryContent)

type RetryConsumer struct {
	log *zap.Logger
	*producer.Retry
	problemRepo repository.ProblemRepository
	ContestRepo repository.ContestRepository

	FuncMap map[string]fn
}

func NewRetryConsumer(log *zap.Logger, p *producer.Retry, problem repository.ProblemRepository, c repository.ContestRepository) mq.Consumer {
	r := RetryConsumer{
		log:         log,
		Retry:       p,
		problemRepo: problem,
		ContestRepo: c,
		FuncMap:     make(map[string]fn, 32),
	}
	//重试函数列表
	r.FuncMap["MongoUpdateInfoByObjID"] = r.MongoUpdateInfoByObjID

	return &r
}

func (rc *RetryConsumer) Consume(ctx context.Context) {
	rc.log.Info("start consume retry")
	defer rc.log.Info("end consume retry")
	msgs, err := rc.Channel.Consume(rc.QueueName, "", false, false, false, false, nil)
	if err != nil {
		rc.log.Error("consume  error", zap.Error(err))
		return
	}
	// Ack(true)表示确认之前的所有信息 false只确认当前信息
	// Nack(false, true)表示重新入队 (false, false)加入死信队列
	workerPool := make(chan struct{}, 128)
	for msg := range msgs {
		workerPool <- struct{}{}
		go func(msg amqp.Delivery) {
			defer func() { <-workerPool }()
			detail := producer.RetryContent{}
			err = json.Unmarshal(msg.Body, &detail)
			if err != nil {
				rc.log.Error("unmarshal contest error", zap.Error(err))
				//无法解析直接进死信
				msg.Nack(false, false)
				return
			}
			f, ok := rc.FuncMap[detail.FuncName]
			if !ok {
				rc.log.Error("func not exist", zap.String("func", detail.FuncName))
				msg.Nack(false, false)
				return
			}
			//执行对应重试函数
			f(ctx, msg, detail)
		}(msg)
	}
}

func (rc *RetryConsumer) MongoUpdateInfoByObjID(ctx context.Context, msg amqp.Delivery, detail producer.RetryContent) {
	problem := &entity.Problem{}
	err := json.Unmarshal([]byte(detail.Params), problem)
	//断言失败 不能调用这个函数
	if err != nil || problem == nil {
		rc.log.Error("unmarshal problem param", zap.String("func", detail.FuncName))
		msg.Nack(false, false)
		return
	}

	for _, seconds := range mq.ReTryDelaySeconds {
		//延迟重试
		time.Sleep(time.Duration(seconds) * time.Second)

		if err = rc.problemRepo.MongoUpdateInfoByObjID(ctx, problem, detail.ObjectID); err != nil {
			rc.log.Error("func exec failed", zap.String("func", detail.FuncName), zap.Error(err))
			continue
		}
		//没有continue则重试完成

		msg.Ack(false)
		return

	}
	rc.log.Error("重试到达上限, 加入死信队列", zap.String("func", detail.FuncName))
	msg.Nack(false, false)

}
