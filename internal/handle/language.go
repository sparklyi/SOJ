package handle

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/service"
	"SOJ/utils"
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"strconv"
)

type LanguageHandle struct {
	log *zap.Logger
	svc *service.LanguageService
}

// NewLanguageHandle 依赖注入
func NewLanguageHandle(log *zap.Logger, svc *service.LanguageService) *LanguageHandle {
	return &LanguageHandle{
		log: log,
		svc: svc,
	}
}

// GetInfo 获取测评语言信息
func (lh *LanguageHandle) GetInfo(ctx *gin.Context) {
	t := ctx.Param("lid")
	lid, err := strconv.Atoi(t)
	if err != nil || lid <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	lang, err := lh.svc.GetInfo(ctx, lid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, lang)

}

// List 获取语言列表
func (lh *LanguageHandle) List(ctx *gin.Context) {
	req := entity.Language{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	claims := utils.GetAccessClaims(ctx)
	//普通用户只能查看可用语言
	if claims.Auth <= constant.AdminLevel {
		req.Status = new(bool)
		*req.Status = true
	}

	lang, err := lh.svc.GetLanguageList(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, lang)
}

// Update 测评语言更新
func (lh *LanguageHandle) Update(ctx *gin.Context) {
	req := entity.Language{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	err := lh.svc.UpdateLang(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// SyncLanguage 同步judge0的测评语言
func (lh *LanguageHandle) SyncLanguage(ctx *gin.Context) {
	err := lh.svc.SyncJudge0Lang(ctx)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}
