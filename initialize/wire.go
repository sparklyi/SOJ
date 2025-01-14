//go:build wireinject

package initialize

import (
	"SOJ/internal/api"
	"SOJ/internal/mq"
	"SOJ/pkg/email"
	"SOJ/utils/jwt"
	"github.com/google/wire"
)

func InitServer() *Cmd {

	//wire会自动排列初始化顺序
	wire.Build(
		InitLogger,
		InitRedis,
		InitMiddleware,
		jwt.New,
		email.New,
		mq.NewEmailProducer,
		mq.NewEmailConsumer,
		api.NewUserAPI,
		wire.Struct(new(Cmd), "*"),
	)
	return new(Cmd)
}
