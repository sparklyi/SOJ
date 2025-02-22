package handle

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/service"
	"SOJ/utils"
	"SOJ/utils/jwt"
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"

	"go.uber.org/zap"
	"strconv"
)

type UserHandle interface {
	Register(ctx *gin.Context)
	LoginByEmail(ctx *gin.Context)
	LoginByPassword(ctx *gin.Context)
	RefreshToken(ctx *gin.Context)
	Logout(ctx *gin.Context)
	GetUserInfo(ctx *gin.Context)
	UploadAvatar(ctx *gin.Context)
	UpdatePassword(ctx *gin.Context)
	GetUserList(ctx *gin.Context)
	UpdateUserInfo(ctx *gin.Context)
	ResetPassword(ctx *gin.Context)
	Delete(ctx *gin.Context)
}

type user struct {
	jwt *jwt.JWT
	log *zap.Logger
	svc service.UserService
}

// NewUserHandle 依赖注入方法
func NewUserHandle(log *zap.Logger, jwt *jwt.JWT, s service.UserService) UserHandle {
	return &user{
		jwt: jwt,
		log: log,
		svc: s,
	}
}

// Register 用户注册
func (uh *user) Register(ctx *gin.Context) {
	req := entity.Register{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	//转service层服务
	um, err := uh.svc.Register(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	//生成token
	st, rt, err := uh.jwt.CreateToken(ctx, int(um.ID), um.Role)
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
func (uh *user) LoginByEmail(ctx *gin.Context) {
	req := entity.LoginByEmail{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	um, err := uh.svc.LoginByEmail(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	st, rt, err := uh.jwt.CreateToken(ctx, int(um.ID), um.Role)
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

// LoginByPassword 密码登录
func (uh *user) LoginByPassword(ctx *gin.Context) {
	req := entity.LoginByPassword{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	u, err := uh.svc.LoginByPassword(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	st, rt, err := uh.jwt.CreateToken(ctx, int(u.ID), u.Role)
	if err != nil {
		response.InternalErrorWithMsg(ctx, "令牌生成失败")
		return
	}
	response.SuccessWithData(ctx, map[string]interface{}{
		"id":                u.ID,
		"SOJ-Access-Token":  st,
		"SOJ-Refresh-Token": rt,
	})
}

// RefreshToken 刷新token令牌
func (uh *user) RefreshToken(ctx *gin.Context) {

	rt := ctx.GetHeader("SOJ-Refresh-Token")
	if rt == "" {
		response.BadRequestErrorWithMsg(ctx, "未携带刷新token")
		return
	}

	//验证长token是否有效
	claims, err := uh.jwt.VerifyRefresh(ctx, rt)
	if err != nil {
		response.BadRequestErrorWithMsg(ctx, err.Error())
		return
	}
	//生成新的token
	token, err := uh.jwt.CreateAccessToken(ctx, claims.ID, claims.Auth)
	if err != nil {
		response.InternalErrorWithMsg(ctx, "访问令牌生成失败")
		return
	}
	response.SuccessWithData(ctx, map[string]interface{}{
		"SOJ-Access-Token": token,
	})

}

// Logout 用户手动退出
func (uh *user) Logout(ctx *gin.Context) {
	rt := ctx.GetHeader("SOJ-Refresh-Token")
	if rt == "" {
		response.BadRequestErrorWithMsg(ctx, "未携带刷新token")
		return
	}
	//令长token失效
	err := uh.jwt.BanToken(ctx, rt)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// GetUserInfo 获取用户信息
func (uh *user) GetUserInfo(ctx *gin.Context) {
	tid := ctx.Param("id")

	if tid == "" {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	id, err := strconv.Atoi(tid)
	if err != nil || id <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	u, err := uh.svc.GetUserByID(ctx, id)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, u)
}

// UploadAvatar 头像上传至cos
func (uh *user) UploadAvatar(ctx *gin.Context) {
	claims := utils.GetAccessClaims(ctx)
	if claims == nil {
		response.UnauthorizedErrorWithMsg(ctx, constant.UnauthorizedError)
		return
	}
	avatar, err := ctx.FormFile("avatar")
	if err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}

	if err = uh.svc.UploadAvatar(ctx, avatar, claims.ID); err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)

}

// UpdatePassword 修改密码
func (uh *user) UpdatePassword(ctx *gin.Context) {
	req := entity.UpdatePassword{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	if err := uh.svc.UpdatePassword(ctx, &req); err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// GetUserList 获取用户信息列表
func (uh *user) GetUserList(ctx *gin.Context) {
	req := &entity.UserInfo{}
	if err := ctx.ShouldBind(req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	users, err := uh.svc.GetUserList(ctx, req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, users)
}

// UpdateUserInfo 用户信息更新
func (uh *user) UpdateUserInfo(ctx *gin.Context) {
	req := &entity.UserUpdate{}
	if err := ctx.ShouldBind(req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	claims := utils.GetAccessClaims(ctx)

	if claims == nil || (claims.ID != req.ID && claims.Auth != constant.RootLevel) {
		response.UnauthorizedErrorWithMsg(ctx, constant.UnauthorizedError)
		return
	}
	err := uh.svc.UpdateUserInfo(ctx, req, claims.Auth == 3)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// ResetPassword 重置密码
func (uh *user) ResetPassword(ctx *gin.Context) {
	e := ctx.Param("email")
	if e == "" {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	err := uh.svc.ResetPassword(ctx, e)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)

}

// Delete 用户删除
func (uh *user) Delete(ctx *gin.Context) {
	t := ctx.Param("id")
	id, err := strconv.Atoi(t)
	if err != nil || id <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	err = uh.svc.DeleteByID(ctx, id)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}
