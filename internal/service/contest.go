package service

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/repository"
	"SOJ/utils"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type ContestService struct {
	log       *zap.Logger
	repo      *repository.ContestRepository
	applyRepo *repository.ApplyRepository
}

// NewContestService 依赖注入
func NewContestService(logger *zap.Logger, repo *repository.ContestRepository, a *repository.ApplyRepository) *ContestService {
	return &ContestService{
		log:       logger,
		repo:      repo,
		applyRepo: a,
	}

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
	claims := utils.GetAccessClaims(ctx)
	if c.UserID != uint(claims.ID) && claims.Auth < constant.AdminLevel {
		return errors.New(constant.UnauthorizedError)
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

// GetContestList 获取比赛列表
func (cs *ContestService) GetContestList(ctx *gin.Context, req *entity.ContestList) ([]*model.Contest, error) {
	claims := utils.GetAccessClaims(ctx)
	admin := claims != nil && claims.Auth > constant.UserLevel

	list, err := cs.repo.GetContestList(ctx, req, admin)
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		v.Code = ""
		v.FreezeTime = nil
		v.Description = ""
		v.ProblemSet = ""
	}
	return list, nil
}

// GetListByUserID 获取用户比赛列表
func (cs *ContestService) GetListByUserID(ctx *gin.Context, id, page, pageSize int) ([]*model.Contest, error) {
	return cs.repo.GetListByUserID(ctx, id, page, pageSize)
}

// GetContestInfoByID 获取比赛详情
func (cs *ContestService) GetContestInfoByID(ctx *gin.Context, id int) (*model.Contest, error) {
	c, err := cs.repo.GetContestInfoByID(ctx, id)
	if err != nil {
		return nil, err
	}
	claims := utils.GetAccessClaims(ctx)
	if claims.Auth < constant.AdminLevel && c.UserID != uint(claims.ID) {
		c.Code = ""
	}
	return c, nil
}

// DeleteContest 删除比赛
func (cs *ContestService) DeleteContest(ctx *gin.Context, id int) error {
	c, err := cs.repo.GetContestInfoByID(ctx, id)
	if err != nil {
		return err
	}
	claims := utils.GetAccessClaims(ctx)
	if claims.Auth < constant.AdminLevel && c.UserID != uint(claims.ID) {
		return errors.New(constant.UnauthorizedError)
	}
	err = cs.repo.DeleteContest(ctx, id)
	if err != nil {
		return err
	}
	go cs.applyRepo.DeleteApplyByContestID(ctx, id)
	return nil
}
