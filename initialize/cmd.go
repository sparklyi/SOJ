package initialize

import (
	"SOJ/internal/mq"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
)

type Cmd struct {
	G             *gin.Engine
	EmailConsumer *mq.EmailConsumer
	Cron          *cron.Cron
}
