package service

import (
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/repository"
	"SOJ/utils"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type ContestService struct {
	log  *zap.Logger
	repo *repository.ContestRepository
}

// NewContestService 依赖注入
func NewContestService(logger *zap.Logger, repo *repository.ContestRepository) *ContestService {
	return &ContestService{log: logger, repo: repo}

}

// CreateContest 创建比赛
func (cs *ContestService) CreateContest(ctx *gin.Context, req *entity.Contest) (*model.Contest, error) {
	problemSet, _ := json.Marshal(req.ProblemSet)
	if req.Public == nil {
		req.Code = utils.GenerateRandCode(6)
	}
	c := &model.Contest{
		Name:        req.Name,
		UserID:      uint(utils.GetAccessClaims(ctx).ID),
		Tag:         req.Tag,
		Type:        req.Type,
		Sponsor:     req.Sponsor,
		Description: req.Description,
		ProblemSet:  string(problemSet),
		Public:      req.Public,
		Code:        req.Code,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
		FreezeTime:  req.FreezeTime,
	}
	return cs.repo.CreateContest(ctx, c)
}
