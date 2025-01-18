package handle

import (
	"SOJ/internal/entity"
	"SOJ/internal/service"
	"SOJ/utils/jwt"
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type UserHandler struct {
	jwt *jwt.JWT
	log *zap.Logger
	svc *service.UserService
}

func NewUserHandler(log *zap.Logger, jwt *jwt.JWT, s *service.UserService) *UserHandler {
	return &UserHandler{
		jwt: jwt,
		log: log,
		svc: s,
	}
}

func (u *UserHandler) Register(ctx *gin.Context) {
	req := entity.Register{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, err.Error())
		response.BadRequestErrorWithMsg(ctx, "参数错误")

		return
	}
	//转service层服务
	um, err := u.svc.Register(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	//生成token
	st, rt, err := u.jwt.CreateToken(ctx, int(um.ID), um.Role)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, map[string]interface{}{
		"id":            um.ID,
		"access_token":  st,
		"refresh_token": rt,
	})

}
