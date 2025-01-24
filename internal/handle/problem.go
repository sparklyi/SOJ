package handle

import (
	"SOJ/internal/service"
	"go.uber.org/zap"
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
