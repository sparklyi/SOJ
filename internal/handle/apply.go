package handle

import (
	"SOJ/internal/service"
	"go.uber.org/zap"
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
