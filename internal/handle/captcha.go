package handle

import (
	"SOJ/utils/captcha"
	"SOJ/utils/response"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type CaptchaHandle struct {
	captcha *captcha.Captcha
	log     *zap.Logger
}

func NewCaptchaHandle(c *captcha.Captcha, log *zap.Logger) *CaptchaHandle {
	return &CaptchaHandle{
		captcha: c,
		log:     log,
	}
}

func (c *CaptchaHandle) GenerateCaptcha(ctx *gin.Context) {

	captchaID, base64, digit, err := c.captcha.Generate()
	if err != nil {
		c.log.Error("验证码生成失败", zap.Error(err))
		response.InternalErrorWithMsg(ctx, "验证码生成失败")
		return
	}

	fmt.Println(digit)
	response.SuccessWithData(ctx, map[string]interface{}{
		"captcha_id":     captchaID,
		"captcha_base64": base64,
	})

}
