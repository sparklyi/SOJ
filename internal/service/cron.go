package service

import (
	"context"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"time"
)

func NewCronTask(
	log *zap.Logger,
	lang *LanguageService,
	submission *SubmissionService,
) *cron.Cron {

	c := cron.New()
	//minute hour day month weekday(0为星期天)
	// 1 * * * * 每小时第1分钟
	// 0 1 * * 1 每周一的1点0分
	// * 1 1 * 1 每个月1号的1点每分钟执行 并且1号要是星期1
	var err error
	//每月1号1点0分执行
	_, err = c.AddFunc("0 1 1 * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = lang.SyncJudge0Lang(ctx)
		if err != nil {
			log.Error("定时任务-测评语言同步:失败")
		} else {
			log.Info("定时任务-测评语言同步:成功")
		}

	})
	if err != nil {
		panic(err)
	}
	//每月1号2点0分执行
	_, err = c.AddFunc("0 2 1 * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = submission.DeletePostgresJudgeHistory(ctx)
		if err != nil {
			log.Error("定时任务-postgres测评记录删除:失败")
		} else {
			log.Info("定时任务-postgres测评记录删除:成功")
		}

	})
	if err != nil {
		panic(err)
	}
	return c
}
