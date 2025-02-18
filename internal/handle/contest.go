package handle

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/service"
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type ContestHandle struct {
	log *zap.Logger
	svc *service.ContestService
}

// NewContestHandle 依赖注入
func NewContestHandle(log *zap.Logger, svc *service.ContestService) *ContestHandle {
	return &ContestHandle{
		log: log,
		svc: svc,
	}

}

// CreateContest 创建比赛
func (ch *ContestHandle) CreateContest(ctx *gin.Context) {
	req := entity.Contest{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	if req.StartTime.Unix() > req.EndTime.Unix() {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	c, err := ch.svc.CreateContest(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, c)

}

// UpdateContest 更新比赛
func (ch *ContestHandle) UpdateContest(ctx *gin.Context) {

	req := entity.Contest{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	if req.StartTime.Unix() > req.EndTime.Unix() {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}

	if err := ch.svc.UpdateContest(ctx, &req); err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}
