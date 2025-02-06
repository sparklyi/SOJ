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

type UserHandle struct {
	jwt *jwt.JWT
	log *zap.Logger
	svc *service.UserService
}

// NewUserHandle 依赖注入方法
func NewUserHandle(log *zap.Logger, jwt *jwt.JWT, s *service.UserService) *UserHandle {
	return &UserHandle{
		jwt: jwt,
		log: log,
		svc: s,
	}
}

// Register 用户注册
func (u *UserHandle) Register(ctx *gin.Context) {
	req := entity.Register{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数无效")
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
func (u *UserHandle) LoginByEmail(ctx *gin.Context) {
	req := entity.LoginByEmail{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数无效")
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

// LoginByPassword 密码登录
func (u *UserHandle) LoginByPassword(ctx *gin.Context) {
	req := entity.LoginByPassword{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数无效")
		return
	}
	user, err := u.svc.LoginByPassword(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	st, rt, err := u.jwt.CreateToken(ctx, int(user.ID), user.Role)
	if err != nil {
		response.InternalErrorWithMsg(ctx, "令牌生成失败")
		return
	}
	response.SuccessWithData(ctx, map[string]interface{}{
		"id":                user.ID,
		"SOJ-Access-Token":  st,
		"SOJ-Refresh-Token": rt,
	})
}

// RefreshToken 刷新token令牌
func (u *UserHandle) RefreshToken(ctx *gin.Context) {

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
func (u *UserHandle) Logout(ctx *gin.Context) {
	rt := ctx.GetHeader("SOJ-Refresh-Token")
	if rt == "" {
		response.BadRequestErrorWithMsg(ctx, "未携带刷新token")
		return
	}
	//令长token失效
	err := u.jwt.BanToken(ctx, rt)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// GetUserInfo 获取用户信息
func (u *UserHandle) GetUserInfo(ctx *gin.Context) {
	tid := ctx.Param("id")

	if tid == "" {
		response.BadRequestErrorWithMsg(ctx, "未提供id")
		return
	}
	id, err := strconv.Atoi(tid)
	if err != nil || id <= 0 {
		response.BadRequestErrorWithMsg(ctx, "参数无效")
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
func (u *UserHandle) UploadAvatar(ctx *gin.Context) {
	claims := utils.GetAccessClaims(ctx)
	if claims == nil {
		response.UnauthorizedErrorWithMsg(ctx, "未授权")
		return
	}
	avatar, err := ctx.FormFile("avatar")
	if err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数无效")
		return
	}

	if err = u.svc.UploadAvatar(ctx, avatar, claims.ID); err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)

}

// UpdatePassword 修改密码
func (u *UserHandle) UpdatePassword(ctx *gin.Context) {
	req := entity.UpdatePassword{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数无效")
		return
	}
	if err := u.svc.UpdatePassword(ctx, &req); err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// GetUserList 获取用户信息列表
func (u *UserHandle) GetUserList(ctx *gin.Context) {
	req := &entity.UserInfo{}
	if err := ctx.ShouldBind(req); err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数无效")
		return
	}
	users, err := u.svc.GetUserList(ctx, req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, users)
}

// UpdateUserInfo 用户信息更新
func (u *UserHandle) UpdateUserInfo(ctx *gin.Context) {
	req := &entity.UserUpdate{}
	if err := ctx.ShouldBind(req); err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数无效")
		return
	}
	claims := utils.GetAccessClaims(ctx)

	if claims == nil || (claims.ID != req.ID && claims.Auth != constant.RootLevel) {
		response.UnauthorizedErrorWithMsg(ctx, "未授权")
		return
	}
	err := u.svc.UpdateUserInfo(ctx, req, claims.Auth == 3)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// ResetPassword 重置密码
func (u *UserHandle) ResetPassword(ctx *gin.Context) {
	email := ctx.Param("email")
	if email == "" {
		response.BadRequestErrorWithMsg(ctx, "参数无效")
		return
	}
	err := u.svc.ResetPassword(ctx, email)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)

}
