package initialize

import (
	"SOJ/internal/mq"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
)

type Cmd struct {
	G        *gin.Engine
	Consumer []mq.Consumer
	Cron     *cron.Cron
}
