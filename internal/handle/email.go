package handle

import (
	"SOJ/internal/entity"
	"SOJ/internal/mq"
	"SOJ/utils"
	"github.com/gin-gonic/gin"
	"net/http"
)

type EmailHandler struct {
	emailProducer *mq.EmailProducer
}

func NewEmailHandler(e *mq.EmailProducer) *EmailHandler {
	return &EmailHandler{e}
}

func (e *EmailHandler) SendEmailCode(c *gin.Context) {
	req := entity.SendEmailCode{}
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "参数无效"})
		return
	}
	code := utils.GenerateRandCode(6)
	data := mq.EmailContent{
		Target:  []string{req.Email},
		Subject: "注册验证",
		Content: `<h1>欢迎注册SOJ!</h1><br>你的注册码为<a>` + code + `</a><br>验证码有效时限为1分钟,请勿泄露于他人!`,
		Code:    code,
	}
	err := e.emailProducer.Send(c, data, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": "验证码发送失败"})
	}

}
