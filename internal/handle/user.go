package handle

import (
	"SOJ/internal/entity"
	"SOJ/internal/service"
	"SOJ/utils"
	"SOJ/utils/jwt"
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"

	"go.uber.org/zap"
	"strconv"
)

type UserHandler struct {
	jwt *jwt.JWT
	log *zap.Logger
	svc *service.UserService
}

// NewUserHandler 依赖注入方法
func NewUserHandler(log *zap.Logger, jwt *jwt.JWT, s *service.UserService) *UserHandler {
	return &UserHandler{
		jwt: jwt,
		log: log,
		svc: s,
	}
}

// Register 用户注册
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
		"id":                um.ID,
		"SOJ-Access-Token":  st,
		"SOJ-Refresh-Token": rt,
	})

}

// LoginByEmail 使用邮箱登录
func (u *UserHandler) LoginByEmail(ctx *gin.Context) {
	req := entity.LoginByEmail{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数错误")
		return
	}
	um, err := u.svc.LoginByEmail(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	st, rt, err := u.jwt.CreateToken(ctx, int(um.ID), um.Role)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, map[string]interface{}{
		"id":                um.ID,
		"SOJ-Access-Token":  st,
		"SOJ-Refresh-Token": rt,
	})

}

// RefreshToken 刷新token令牌
func (u *UserHandler) RefreshToken(ctx *gin.Context) {

	rt := ctx.GetHeader("SOJ-Refresh-Token")
	if rt == "" {
		response.BadRequestErrorWithMsg(ctx, "未携带刷新token")
		return
	}

	//验证长token是否有效
	claims, err := u.jwt.VerifyRefresh(ctx, rt)
	if err != nil {
		response.BadRequestErrorWithMsg(ctx, err.Error())
		return
	}
	//生成新的token
	token, err := u.jwt.CreateAccessToken(ctx, claims.ID, claims.Auth)
	if err != nil {
		response.InternalErrorWithMsg(ctx, "访问令牌生成失败")
		return
	}
	response.SuccessWithData(ctx, map[string]interface{}{
		"SOJ-Access-Token": token,
	})

}

// Logout 用户手动退出
func (u *UserHandler) Logout(ctx *gin.Context) {
	rt := ctx.GetHeader("SOJ-Refresh-Token")
	if rt == "" {
		response.BadRequestErrorWithMsg(ctx, "未携带刷新token")
		return
	}
	err := u.jwt.BanToken(ctx, rt)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

func (u *UserHandler) GetUserInfo(ctx *gin.Context) {
	tid := ctx.Param("id")

	if tid == "" {
		response.BadRequestErrorWithMsg(ctx, "未提供id")
		return
	}
	id, err := strconv.Atoi(tid)
	if err != nil || id <= 0 {
		response.BadRequestErrorWithMsg(ctx, "参数错误")
		return
	}
	user, err := u.svc.GetUserByID(ctx, id)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, user)
}

// UploadAvatar 头像上传至cos
func (u *UserHandler) UploadAvatar(ctx *gin.Context) {
	claims := utils.GetAccessClaims(ctx)
	if claims == nil {
		response.UnauthorizedErrorWithMsg(ctx, "未授权")
		return
	}
	avatar, err := ctx.FormFile("avatar")
	if err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数错误")
		return
	}

	if err = u.svc.UploadAvatar(ctx, avatar, claims.ID); err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)

}
