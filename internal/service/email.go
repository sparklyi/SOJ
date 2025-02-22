package service

import (
	"SOJ/internal/entity"
	"SOJ/internal/mq"
	"SOJ/utils"
	"SOJ/utils/captcha"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type EmailService interface {
	SendVerifyCode(ctx *gin.Context, req *entity.SendEmailCode) error
	CheckCaptcha(c *gin.Context, req *entity.SendEmailCode) bool
}

type email struct {
	log           *zap.Logger
	rs            *redis.Client
	EmailProducer *mq.EmailProducer
	captcha       *captcha.Captcha
}

func NewEmailService(log *zap.Logger, rs *redis.Client, e *mq.EmailProducer, c *captcha.Captcha) EmailService {
	return &email{
		log:           log,
		rs:            rs,
		EmailProducer: e,
		captcha:       c,
	}
}

func (es *email) SendVerifyCode(ctx *gin.Context, req *entity.SendEmailCode) error {
	//随机生成
	code := utils.GenerateRandCode(6)
	fmt.Println(code)
	data := mq.EmailContent{
		Target:  []string{req.Email},
		Subject: "验证码",
		Content: `<h1>SOJ</h1><br>你的验证码为<a>` + code + `</a><br>验证码有效时限为1分钟,请勿泄露于他人!`,
		Code:    code,
	}
	//redis是否已经缓存过验证码
	exist, err := es.rs.Exists(ctx, req.Email).Result()
	if err != nil {
		return err
	}
	if exist != 0 {
		return errors.New("验证码未过期")
	}
	err = es.EmailProducer.Send(ctx, data, 0)
	if err != nil {
		es.log.Error("生产者消息生产失败", zap.Error(err))
		return errors.New("验证码发送失败")
	}
	return nil
}

// CheckCaptcha 图形验证码校验
func (es *email) CheckCaptcha(c *gin.Context, req *entity.SendEmailCode) bool {
	return es.captcha.Verify(req.CaptchaID, req.Captcha, true)
}
