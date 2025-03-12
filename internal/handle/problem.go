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

type ProblemHandle interface {
	List(ctx *gin.Context)
	Detail(ctx *gin.Context)
	Count(ctx *gin.Context)
	Create(ctx *gin.Context)
	UpdateInfo(ctx *gin.Context)
	Delete(ctx *gin.Context)
	TestCaseInfo(ctx *gin.Context)
	CreateTestCase(ctx *gin.Context)
	UpdateTestCase(ctx *gin.Context)
}

type problem struct {
	log *zap.Logger
	svc service.ProblemService
}

func NewProblemHandle(log *zap.Logger, svc service.ProblemService) ProblemHandle {
	return &problem{
		log: log,
		svc: svc,
	}
}

// List 题库列表
func (ph *problem) List(ctx *gin.Context) {
	req := entity.ProblemList{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数无效")
		return
	}
	ps, count, err := ph.svc.GetProblemList(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}

	response.SuccessWithData(ctx, gin.H{
		"detail": ps,
		"count":  count,
	})

}

func (ph *problem) Detail(ctx *gin.Context) {
	t := ctx.Param("pid")
	pid, err := strconv.Atoi(t)
	if err != nil || pid <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	data, err := ph.svc.GetProblemInfo(ctx, pid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, data)

}

// Count 题目总数
func (ph *problem) Count(ctx *gin.Context) {
	total, err := ph.svc.Count(ctx)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, total)

}

// Create 创建题目
func (ph *problem) Create(ctx *gin.Context) {
	req := entity.Problem{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, "参数无效"+err.Error())
		return
	}
	p, err := ph.svc.Create(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, p)

}

// UpdateInfo  题目信息更新
func (ph *problem) UpdateInfo(ctx *gin.Context) {
	req := entity.Problem{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError+err.Error())
		return
	}
	err := ph.svc.UpdateProblemInfo(ctx, &req)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)

}

// Delete  题目删除
func (ph *problem) Delete(ctx *gin.Context) {
	t := ctx.Param("pid")
	pid, err := strconv.Atoi(t)
	if err != nil || pid <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	err = ph.svc.DeleteProblem(ctx, pid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// TestCaseInfo 获取题目测试点
func (ph *problem) TestCaseInfo(ctx *gin.Context) {
	t := ctx.Param("pid")
	pid, err := strconv.Atoi(t)
	if err != nil || pid <= 0 {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	res, err := ph.svc.GetTestCaseInfo(ctx, pid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessWithData(ctx, res)

}

// CreateTestCase 创建测试点
func (ph *problem) CreateTestCase(ctx *gin.Context) {
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
	err = ph.svc.CreateTestCase(ctx, &req, pid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)
}

// UpdateTestCase 更新测试点
func (ph *problem) UpdateTestCase(ctx *gin.Context) {
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
	err = ph.svc.UpdateTestCase(ctx, &req, pid)
	if err != nil {
		response.InternalErrorWithMsg(ctx, err.Error())
		return
	}
	response.SuccessNoContent(ctx)

}
