package service

import (
	"context"
	"fmt"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"time"
)

func NewCronTask(
	log *zap.Logger,
	lang *LanguageService,
) *cron.Cron {

	c := cron.New()
	//minute hour day month weekday(0为星期天)
	// 1 * * * * 每小时第1分钟
	// 0 1 * * 1 每周一的1点0分
	// * 1 1 * 1 每个月1号的1点每分钟执行 并且1号要是星期1
	_, err := c.AddFunc("0 1 * * 1", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := lang.SyncJudge0Lang(ctx)
		if err != nil {
			log.Error("同步任务-测评语言同步:失败", zap.Error(err))
		} else {
			fmt.Println("同步任务-测评语言同步完成")
		}

	})
	if err != nil {
		panic(err)
	}
	return c
}
