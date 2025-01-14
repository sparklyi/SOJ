package initialize

import (
	"SOJ/internal/api"
	"SOJ/internal/mq"
)

type Cmd struct {
	*api.UserAPI
	*mq.EmailConsumer
}
