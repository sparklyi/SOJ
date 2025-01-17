package handle

import (
	"SOJ/internal/entity"
	"SOJ/internal/service"
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"
)

type EmailHandler struct {
	svc *service.EmailService
}

func NewEmailHandler(se *service.EmailService) *EmailHandler {
	return &EmailHandler{se}
}

func (e *EmailHandler) SendEmailCode(c *gin.Context) {
	req := entity.SendEmailCode{}
	if err := c.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(c, "参数无效")
		return
	}
	if err := e.svc.SendEmailCode(c, &req); err != nil {
		response.BadRequestErrorWithMsg(c, err.Error())
	}
	response.SuccessNoContent(c)

}
