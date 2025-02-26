//go:build wireinject

package initialize

import (
	"SOJ/internal/handle"
	"SOJ/internal/mq/consumer"
	"SOJ/internal/mq/producer"
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
		InitRabbitMQ,
		InitConsumer,
		InitCos,
		jwt.New,
		captcha.New,
		captcha.NewRedisStore,
		email.New,
		judge0.New,
		producer.NewEmailProducer,
		producer.NewContestProducer,
		consumer.NewContestConsumer,
		consumer.NewEmailConsumer,
		handle.NewEmailHandle,
		handle.NewUserHandle,
		handle.NewCaptchaHandle,
		handle.NewProblemHandle,
		handle.NewLanguageHandle,
		handle.NewSubmissionHandle,
		handle.NewContestHandle,
		handle.NewApplyHandle,
		service.NewUserService,
		service.NewEmailService,
		service.NewProblemService,
		service.NewLanguageService,
		service.NewSubmissionService,
		service.NewContestService,
		service.NewApplyService,
		repository.NewUserRepository,
		repository.NewProblemRepository,
		repository.NewLanguageRepository,
		repository.NewSubmissionRepository,
		repository.NewContestRepository,
		repository.NewApplyRepository,
		InitRoute,
		service.NewCronTask,
		wire.Struct(new(Cmd), "*"),
	)
	return new(Cmd)
}
