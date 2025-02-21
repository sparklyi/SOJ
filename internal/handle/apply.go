package handle

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/service"
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"strconv"
)

type ApplyHandle struct {
	log *zap.Logger
	svc *service.ApplyService
}

func NewApplyHandle(log *zap.Logger, svc *service.ApplyService) *ApplyHandle {
	return &ApplyHandle{
		log: log,
		svc: svc,
	}
}

// CreateApply 创建报名
func (ah *ApplyHandle) CreateApply(ctx *gin.Context) {
	req := entity.Apply{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	apply, err := ah.svc.CreateApply(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, apply)
}

// UpdateApply 更新报名
func (ah *ApplyHandle) UpdateApply(ctx *gin.Context) {
	req := entity.Apply{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	err := ah.svc.UpdateApply(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)

}

// DeleteApply 取消报名
func (ah *ApplyHandle) DeleteApply(ctx *gin.Context) {
	t := ctx.Param("aid")
	aid, err := strconv.Atoi(t)
	if err != nil || aid <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	err = ah.svc.DeleteApply(ctx, aid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// GetListByUserID 获取用户报名信息
func (ah *ApplyHandle) GetListByUserID(ctx *gin.Context) {

	page, err := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	if err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	pageSize, err := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
	if err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}

	applies, err := ah.svc.GetListByUserID(ctx, page, pageSize)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, applies)
}

// GetList 获取报名列表
func (ah *ApplyHandle) GetList(ctx *gin.Context) {
	req := entity.ApplyList{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	applies, err := ah.svc.GetList(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, applies)

}

// GetInfoByID 根据报名id获取详情
func (ah *ApplyHandle) GetInfoByID(ctx *gin.Context) {
	t := ctx.Param("aid")
	aid, err := strconv.Atoi(t)
	if err != nil || aid <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	apply, err := ah.svc.GetInfoByID(ctx, aid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, apply)
}
