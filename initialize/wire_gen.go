// Code generated by Wire. DO NOT EDIT.

//go:generate go run -mod=mod github.com/google/wire/cmd/wire
//go:build !wireinject
// +build !wireinject

package initialize

import (
	"SOJ/internal/handle"
	"SOJ/internal/mq"
	"SOJ/internal/repository"
	"SOJ/internal/service"
	"SOJ/pkg/email"
	"SOJ/utils/jwt"
)

// Injectors from wire.go:

func InitServer() *Cmd {
	logger := InitLogger()
	client := InitRedis()
	emailProducer := mq.NewEmailProducer(logger)
	emailService := service.NewEmailService(logger, client, emailProducer)
	emailHandler := handle.NewEmailHandler(emailService)
	jwtJWT := jwt.New(client, logger)
	db := InitDB()
	userRepository := repository.NewUserRepository(logger, db, client)
	userService := service.NewUserService(logger, userRepository, client)
	userHandler := handle.NewUserHandler(logger, jwtJWT, userService)
	v := InitMiddleware(jwtJWT, logger)
	engine := InitRoute(emailHandler, userHandler, v)
	emailEmail := email.New(logger)
	emailConsumer := mq.NewEmailConsumer(logger, emailEmail, emailProducer, client)
	database := InitMongoDB()
	cmd := &Cmd{
		G:             engine,
		EmailConsumer: emailConsumer,
		Mongo:         database,
		DB:            db,
	}
	return cmd
}
