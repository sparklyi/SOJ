package handle

import (
	"SOJ/internal/entity"
	"SOJ/internal/service"
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"
)

type EmailHandle struct {
	svc *service.EmailService
}

func NewEmailHandle(se *service.EmailService) *EmailHandle {
	return &EmailHandle{se}
}

// SendVerifyCode 验证码发送
func (e *EmailHandle) SendVerifyCode(c *gin.Context) {
	req := entity.SendEmailCode{}
	if err := c.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(c, "参数无效")
		return
	}
	//检查图形验证码
	if !e.svc.CheckCaptcha(c, &req) {
		response.BadRequestErrorWithMsg(c, "图形验证码错误")
		return
	}
	//发送邮件验证码
	if err := e.svc.SendVerifyCode(c, &req); err != nil {
		response.BadRequestErrorWithMsg(c, err.Error())
		return
	}
	response.SuccessNoContent(c)

}
