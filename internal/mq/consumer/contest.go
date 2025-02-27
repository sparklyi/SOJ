package consumer

import (
	"SOJ/internal/constant"
	"SOJ/internal/model"
	"SOJ/internal/mq"
	"SOJ/internal/mq/producer"
	"SOJ/internal/repository"
	"SOJ/pkg/email"
	"context"
	"encoding/json"
	"fmt"
	"github.com/streadway/amqp"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"time"
)

type ContestConsumer struct {
	log   *zap.Logger
	email *email.Email
	*producer.Contest
	contest repository.ContestRepository

	db *gorm.DB
}

func NewContestConsumer(l *zap.Logger, e *email.Email, p *producer.Contest, c repository.ContestRepository, db *gorm.DB) *ContestConsumer {
	return &ContestConsumer{
		log:     l,
		email:   e,
		contest: c,
		Contest: p,
		db:      db,
	}
}

func (c *ContestConsumer) Consume(ctx context.Context) {
	c.log.Info("start consume contest")
	defer c.log.Info("end consume contest")
	contents, err := c.Channel.Consume(c.QueueName, "", true, false, false, false, nil)
	if err != nil {
		c.log.Error("consume contest error", zap.Error(err))
		return
	}

	workerPool := make(chan struct{}, 128)
	for msg := range contents {
		workerPool <- struct{}{}
		go func(msg amqp.Delivery) {
			defer func() { <-workerPool }()
			content := producer.ContestNotify{}
			err = json.Unmarshal(msg.Body, &content)
			if err != nil {
				c.log.Error("unmarshal contest error", zap.Error(err))
				return
			}
			for _, seconds := range mq.ReTryDelaySeconds {
				//延迟重试
				time.Sleep(time.Duration(seconds) * time.Second)
				//检查当前比赛是否还存在
				var contest *model.Contest
				contest, err = c.contest.GetContestInfoByID(ctx, int(content.ContestID))
				//比赛不存在或比赛未发布 直接消费完成
				if err != nil && err.Error() == constant.NotFoundError || !*contest.Publish {
					return
				} else if err != nil {
					c.log.Error("获取比赛详情失败, 开始重试", zap.Error(err))
					continue
				}
				//私有比赛只通知报名的用户, 公开比赛通知所有notify属性为1的 & 报名的用户
				addr := make([]string, 0, 256) //存储通知的用户
				//set := make(map[string]struct{}) //去重

				//先获取所有报名的用户的邮箱
				err = c.db.WithContext(ctx).
					Model(&model.Apply{}).
					Select("email").
					Where("contest_id = ?", contest.ID).
					Find(&addr).Error

				if err != nil {
					c.log.Error("获取报名比赛用户邮箱失败,开始重试", zap.Error(err))
					continue
				}

				//公开比赛通知notify=1的用户
				if *contest.Public {
					t := make([]string, 0, 256)
					db := c.db.WithContext(ctx).Model(&model.User{}).Select("email")
					if len(addr) > 0 {
						db = db.Where("email = NOT IN(?)", addr)
					}
					err = db.Where("notify = 1").Find(&t).Error
					if err != nil {
						c.log.Error("获取用户邮箱失败,开始重试", zap.Error(err))
						continue
					}
					addr = append(addr, t...)
				}
				//没有接收方
				if len(addr) == 0 {
					return
				}
				detail := generateContestEmailDetail(contest.Name, *contest.StartTime)
				//发送邮件
				if err = c.email.Send(addr, content.Subject, detail); err != nil {
					c.log.Error("邮件发送异常, 开始重试", zap.Error(err))
					continue
				}
				return
			}
			c.log.Error("消费达到最大次数", zap.Any("content", content))

		}(msg)
	}
}

// generateContestEmailDetail 生成比赛通知邮件的HTML内容
func generateContestEmailDetail(name string, start time.Time) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html lang="zh">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>比赛提醒</title>
    <style>
        body {
            font-family: 'Arial', sans-serif;
            line-height: 1.6;
            color: #333;
            background-color: #f7f7f7;
            margin: 0;
            padding: 20px;
        }
        .container {
            max-width: 600px;
            margin: 0 auto;
            background-color: #ffffff;
            padding: 25px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
        }
        .header {
            text-align: center;
            color: #1E90FF;
            font-size: 26px;
            font-weight: bold;
            margin-bottom: 20px;
        }
        .content {
            font-size: 16px;
            line-height: 1.5;
            margin-bottom: 20px;
        }
        .footer {
            font-size: 14px;
            text-align: center;
            color: #777;
        }
        .footer a {
            color: #1E90FF;
            text-decoration: none;
        }
        .highlight {
            color: #FF4500;
            font-weight: bold;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            比赛提醒
        </div>
        <div class="content">
            <p>亲爱的用户，</p>
            <p>比赛 <span class="highlight">%s</span> 将于 <span class="highlight">%v</span> 开始！</p>
            <p>期待您的参与！</p>
        </div>
        <div class="footer">
            <p>此邮件由 SOJ Team 自动发送。<br>若您不想接收此类消息，请登录官网进行设置。</p>
        </div>
    </div>
</body>
</html>
`, name, start)
}
