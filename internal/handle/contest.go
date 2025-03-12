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

type ContestHandle interface {
	CreateContest(ctx *gin.Context)
	UpdateContest(ctx *gin.Context)
	GetContestList(ctx *gin.Context)
	GetListByUserID(ctx *gin.Context)
	GetInfoByID(ctx *gin.Context)
	DeleteContest(ctx *gin.Context)
	GetRankingList(ctx *gin.Context)
}

type contest struct {
	log *zap.Logger
	svc service.ContestService
}

// NewContestHandle 依赖注入
func NewContestHandle(log *zap.Logger, svc service.ContestService) ContestHandle {
	return &contest{
		log: log,
		svc: svc,
	}

}

// CreateContest 创建比赛
func (ch *contest) CreateContest(ctx *gin.Context) {
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
func (ch *contest) UpdateContest(ctx *gin.Context) {

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

// GetContestList 获取比赛列表
func (ch *contest) GetContestList(ctx *gin.Context) {
	req := entity.ContestList{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	list, count, err := ch.svc.GetContestList(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, gin.H{
		"detail": list,
		"count":  count,
	})
}

// GetListByUserID 获取用户的比赛列表
func (ch *contest) GetListByUserID(ctx *gin.Context) {
	t := ctx.Param("uid")
	uid, err := strconv.Atoi(t)
	if err != nil || uid <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
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
	list, count, err := ch.svc.GetListByUserID(ctx, uid, page, pageSize)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, gin.H{
		"detail": list,
		"count":  count,
	})
}

// GetInfoByID 获取比赛详情
func (ch *contest) GetInfoByID(ctx *gin.Context) {
	t := ctx.Param("cid")
	id, err := strconv.Atoi(t)
	if err != nil || id <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	c, err := ch.svc.GetContestInfoByID(ctx, id)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, c)
}

// DeleteContest 删除比赛
func (ch *contest) DeleteContest(ctx *gin.Context) {
	t := ctx.Param("cid")
	id, err := strconv.Atoi(t)
	if err != nil || id <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	if err = ch.svc.DeleteContest(ctx, id); err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// GetRankingList 获取排行榜
func (ch *contest) GetRankingList(ctx *gin.Context) {
	req := entity.RankingList{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	ranks, count, err := ch.svc.GetRankingList(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, gin.H{
		"detail": ranks,
		"count":  count,
	})

}
