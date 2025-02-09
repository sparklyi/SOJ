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

type ProblemHandle struct {
	log *zap.Logger
	svc *service.ProblemService
}

func NewProblemHandle(log *zap.Logger, svc *service.ProblemService) *ProblemHandle {
	return &ProblemHandle{
		log: log,
		svc: svc,
	}
}

// List 题库列表
func (p *ProblemHandle) List(ctx *gin.Context) {
	req := entity.ProblemList{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数无效")
		return
	}
	ps, err := p.svc.GetProblemList(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}

	response.SuccessWithData(ctx, ps)

}

func (p *ProblemHandle) Detail(ctx *gin.Context) {
	t := ctx.Param("pid")
	pid, err := strconv.Atoi(t)
	if err != nil || pid <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	data, err := p.svc.GetProblemInfo(ctx, pid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, data)

}

// Count 题目总数
func (p *ProblemHandle) Count(ctx *gin.Context) {
	total, err := p.svc.Count(ctx)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, total)

}

// Create 创建题目
func (p *ProblemHandle) Create(ctx *gin.Context) {
	req := entity.Problem{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数无效"+err.Error())
		return
	}
	err := p.svc.Create(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)

}

// UpdateInfo  题目信息更新
func (p *ProblemHandle) UpdateInfo(ctx *gin.Context) {
	req := entity.Problem{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError+err.Error())
		return
	}
	err := p.svc.UpdateProblemInfo(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)

}

// Delete  题目删除
func (p *ProblemHandle) Delete(ctx *gin.Context) {
	t := ctx.Param("pid")
	pid, err := strconv.Atoi(t)
	if err != nil || pid <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	err = p.svc.DeleteProblem(ctx, pid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// TestCaseInfo 获取题目测试点
func (p *ProblemHandle) TestCaseInfo(ctx *gin.Context) {
	t := ctx.Param("pid")
	pid, err := strconv.Atoi(t)
	if err != nil || pid <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	res, err := p.svc.GetTestCaseInfo(ctx, pid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, res)

}

// CreateTestCase 创建测试点
func (p *ProblemHandle) CreateTestCase(ctx *gin.Context) {
	req := entity.TestCase{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	t := ctx.Param("pid")
	pid, err := strconv.Atoi(t)
	if err != nil || pid <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	err = p.svc.CreateTestCase(ctx, &req, pid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// UpdateTestCase 更新测试点
func (p *ProblemHandle) UpdateTestCase(ctx *gin.Context) {
	req := entity.TestCase{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	t := ctx.Param("pid")
	pid, err := strconv.Atoi(t)
	if err != nil || pid <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	err = p.svc.UpdateTestCase(ctx, &req, pid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)

}
