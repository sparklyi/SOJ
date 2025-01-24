//go:build wireinject

package initialize

import (
	"SOJ/internal/handle"
	"SOJ/internal/mq"
	"SOJ/internal/repository"
	"SOJ/internal/service"
	"SOJ/pkg/email"
	"SOJ/utils/captcha"
	"SOJ/utils/jwt"
	"github.com/google/wire"
)

func InitServer() *Cmd {

	//wire会自动排列初始化顺序
	wire.Build(
		InitLogger,
		InitRedis,
		InitMiddleware,
		InitDB,
		InitMongoDB,
		InitCos,
		jwt.New,
		captcha.New,
		captcha.NewRedisStore,
		email.New,
		mq.NewEmailProducer,
		mq.NewEmailConsumer,
		handle.NewEmailHandle,
		handle.NewUserHandle,
		handle.NewCaptchaHandle,
		service.NewUserService,
		service.NewEmailService,
		repository.NewUserRepository,
		InitRoute,
		wire.Struct(new(Cmd), "*"),
	)
	return new(Cmd)
}
