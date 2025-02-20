package service

import (
	"SOJ/internal/repository"
	"go.uber.org/zap"
)

type ApplyService struct {
	log  *zap.Logger
	repo *repository.ApplyRepository
}

func NewApplyService(log *zap.Logger, repo *repository.ApplyRepository) *ApplyService {
	return &ApplyService{
		log:  log,
		repo: repo,
	}
}
