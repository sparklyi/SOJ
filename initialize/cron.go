package initialize

import "github.com/robfig/cron/v3"

func InitCron() *cron.Cron {
	return cron.New()
}
