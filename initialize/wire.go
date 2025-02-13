//go:build wireinject

package initialize

import (
	"SOJ/internal/handle"
	"SOJ/internal/mq"
	"SOJ/internal/repository"
	"SOJ/internal/service"
	"SOJ/pkg/email"
	"SOJ/pkg/judge0"
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
		judge0.New,
		mq.NewEmailProducer,
		mq.NewEmailConsumer,
		handle.NewEmailHandle,
		handle.NewUserHandle,
		handle.NewCaptchaHandle,
		handle.NewProblemHandle,
		handle.NewLanguageHandle,
		handle.NewSubmissionHandle,
		service.NewUserService,
		service.NewEmailService,
		service.NewProblemService,
		service.NewLanguageService,
		service.NewSubmissionService,
		repository.NewUserRepository,
		repository.NewProblemRepository,
		repository.NewLanguageRepository,
		repository.NewSubmissionRepository,
		InitRoute,
		service.NewCronTask,
		wire.Struct(new(Cmd), "*"),
	)
	return new(Cmd)
}
