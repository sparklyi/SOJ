package service

import (
	"SOJ/internal/repository"
	"go.uber.org/zap"
)

type ProblemService struct {
	log  *zap.Logger
	repo *repository.ProblemRepository
}

func NewProblemService(log *zap.Logger, r *repository.ProblemRepository) *ProblemService {
	return &ProblemService{
		log:  log,
		repo: r,
	}
}
