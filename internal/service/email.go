package service

import (
	"SOJ/internal/entity"
	"SOJ/internal/mq"
	"SOJ/utils"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type EmailService struct {
	log           *zap.Logger
	rs            *redis.Client
	emailProducer *mq.EmailProducer
}

func NewEmailService(log *zap.Logger, rs *redis.Client, e *mq.EmailProducer) *EmailService {
	return &EmailService{
		log:           log,
		rs:            rs,
		emailProducer: e,
	}
}

func (es *EmailService) SendRegisterCode(ctx *gin.Context, req *entity.SendEmailCode) error {
	code := utils.GenerateRandCode(6)
	fmt.Println(code)
	data := mq.EmailContent{
		Target:  []string{req.Email},
		Subject: "注册验证",
		Content: `<h1>欢迎注册SOJ!</h1><br>你的注册码为<a>` + code + `</a><br>验证码有效时限为1分钟,请勿泄露于他人!`,
		Code:    code,
	}
	//redis是否已经缓存过验证码
	exist, err := es.rs.Exists(ctx, req.Email).Result()
	if err != nil {
		return err
	}
	if exist == 0 {
		return errors.New("验证码未过期")
	}
	err = es.emailProducer.Send(ctx, data, 0)
	if err != nil {
		es.log.Error("生产者消息生产失败", zap.Error(err))
		return errors.New("验证码发送失败")
	}
	return nil
}

func (es *EmailService) SendResetPwdCode(ctx *gin.Context, req *entity.SendEmailCode) error {
	code := utils.GenerateRandCode(6)
	fmt.Println(code)
	data := mq.EmailContent{
		Target:  []string{req.Email},
		Subject: "密码重置",
		Content: `你的验证码为<a>` + code + `</a><br>验证码有效时限为1分钟,请勿泄露于他人!`,
		Code:    code,
	}
	//redis是否已经缓存过验证码
	exist, err := es.rs.Exists(ctx, req.Email).Result()
	if err != nil {
		return err
	}
	if exist == 0 {
		return errors.New("验证码未过期")
	}
	err = es.emailProducer.Send(ctx, data, 0)
	if err != nil {
		es.log.Error("生产者消息生产失败", zap.Error(err))
		return errors.New("验证码发送失败")
	}
	return nil
}
