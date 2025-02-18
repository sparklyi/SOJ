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
	if req.Public == nil || *req.Public == false {
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
		Publish:     req.Publish,
	}
	return cs.repo.CreateContest(ctx, c)
}

// UpdateContest 更新比赛信息
func (cs *ContestService) UpdateContest(ctx *gin.Context, req *entity.Contest) error {
	c, err := cs.repo.GetContestInfoByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if req.Name != "" {
		c.Name = req.Name
	}
	if req.Description != "" {
		c.Description = req.Description
	}
	if req.Publish != nil {
		c.Publish = req.Publish
	}
	if req.Sponsor != "" {
		c.Sponsor = req.Sponsor
	}
	if req.StartTime != nil {
		c.StartTime = req.StartTime
	}
	if req.EndTime != nil {
		c.EndTime = req.EndTime
	}
	if req.FreezeTime != nil {
		c.FreezeTime = req.FreezeTime
	}
	if req.ProblemSet != nil {
		data, _ := json.Marshal(req.ProblemSet)
		c.ProblemSet = string(data)
	}
	return cs.repo.UpdateContest(ctx, c)

}
