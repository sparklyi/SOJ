package handle

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/service"
	"SOJ/pkg/judge0"
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"net/http"
	"time"
)

type SubmissionHandle struct {
	log *zap.Logger
	svc *service.SubmissionService
}

// NewSubmissionHandle 依赖注入
func NewSubmissionHandle(log *zap.Logger, svc *service.SubmissionService) *SubmissionHandle {
	return &SubmissionHandle{
		log: log,
		svc: svc,
	}
}

func (sh *SubmissionHandle) Run(ctx *gin.Context) {
	req := entity.Run{}
	if err := ctx.ShouldBind(&req); err != nil {
		response.BadRequestErrorWithMsg(ctx, constant.ParamError)
		return
	}
	judge0.Run(&http.Client{
		Timeout: 10 * time.Second,
	}, &req)
}
