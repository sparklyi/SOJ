// Code generated by Wire. DO NOT EDIT.

//go:generate go run -mod=mod github.com/google/wire/cmd/wire
//go:build !wireinject
// +build !wireinject

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
)

// Injectors from wire.go:

func InitServer() *Cmd {
	client := InitRedis()
	logger := InitLogger()
	redisStore := captcha.NewRedisStore(client, logger)
	captchaCaptcha := captcha.New(redisStore)
	captchaHandle := handle.NewCaptchaHandle(captchaCaptcha, logger)
	connection := InitRabbitMQ()
	producerEmail := producer.NewEmailProducer(logger, connection)
	emailService := service.NewEmailService(logger, client, producerEmail, captchaCaptcha)
	emailHandle := handle.NewEmailHandle(emailService)
	jwtJWT := jwt.New(client, logger)
	db := InitDB()
	userRepository := repository.NewUserRepository(logger, db, client)
	cosClient := InitCos()
	userService := service.NewUserService(logger, userRepository, client, cosClient, producerEmail, captchaCaptcha)
	userHandle := handle.NewUserHandle(logger, jwtJWT, userService)
	database := InitMongoDB()
	problemRepository := repository.NewProblemRepository(logger, db, database)
	problemService := service.NewProblemService(logger, problemRepository)
	problemHandle := handle.NewProblemHandle(logger, problemService)
	languageRepository := repository.NewLanguageRepository(logger, db)
	languageService := service.NewLanguageService(logger, languageRepository)
	languageHandle := handle.NewLanguageHandle(logger, languageService)
	applyRepository := repository.NewApplyRepository(logger, db)
	submissionRepository := repository.NewSubmissionRepository(logger, db)
	judge := judge0.New(logger)
	submissionService := service.NewSubmissionService(logger, applyRepository, submissionRepository, problemRepository, languageRepository, judge, userRepository)
	submissionHandle := handle.NewSubmissionHandle(logger, submissionService)
	contestRepository := repository.NewContestRepository(logger, db, database)
	contest := producer.NewContestProducer(logger, connection)
	contestService := service.NewContestService(logger, contestRepository, applyRepository, contest)
	contestHandle := handle.NewContestHandle(logger, contestService)
	applyService := service.NewApplyService(logger, applyRepository, contestRepository)
	applyHandle := handle.NewApplyHandle(logger, applyService)
	v := InitMiddleware(jwtJWT)
	engine := InitRoute(captchaHandle, emailHandle, userHandle, problemHandle, languageHandle, submissionHandle, contestHandle, applyHandle, v)
	emailEmail := email.New(logger)
	contestConsumer := consumer.NewContestConsumer(logger, emailEmail, contest, contestRepository, db)
	emailConsumer := consumer.NewEmailConsumer(logger, emailEmail, producerEmail, client)
	v2 := InitConsumer(contestConsumer, emailConsumer)
	cron := service.NewCronTask(logger, languageService, submissionService)
	cmd := &Cmd{
		G:        engine,
		Consumer: v2,
		Cron:     cron,
	}
	return cmd
}
