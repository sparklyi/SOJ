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

type ContestService interface {
	CreateContest(ctx *gin.Context, req *entity.Contest) (*model.Contest, error)
	UpdateContest(ctx *gin.Context, req *entity.Contest) error
	GetContestList(ctx *gin.Context, req *entity.ContestList) ([]*model.Contest, error)
	GetListByUserID(ctx *gin.Context, id int, page int, pageSize int) ([]*model.Contest, error)
	GetContestInfoByID(ctx *gin.Context, id int) (*model.Contest, error)
	DeleteContest(ctx *gin.Context, id int) error
}

type contest struct {
	log       *zap.Logger
	repo      repository.ContestRepository
	applyRepo repository.ApplyRepository
}

// NewContestService 依赖注入
func NewContestService(logger *zap.Logger, repo repository.ContestRepository, a repository.ApplyRepository) ContestService {
	return &contest{
		log:       logger,
		repo:      repo,
		applyRepo: a,
	}

}

// CreateContest 创建比赛
func (cs *contest) CreateContest(ctx *gin.Context, req *entity.Contest) (*model.Contest, error) {
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
func (cs *contest) UpdateContest(ctx *gin.Context, req *entity.Contest) error {
	c, err := cs.repo.GetContestInfoByID(ctx, req.ID)
	if err != nil {
		return err
	}
	claims := utils.GetAccessClaims(ctx)
	if c.UserID != uint(claims.ID) && claims.Auth < constant.AdminLevel {
		return errors.New(constant.UnauthorizedError)
	}
	j, _ := json.Marshal(req.ProblemSet)
	if req.Public != nil && *req.Public != *c.Public {
		if *req.Public == false {
			req.Code = utils.GenerateRandCode(6)
		}
	}
	data := map[string]interface{}{
		"name":        req.Name,
		"tag":         req.Tag,
		"description": req.Description,
		"publish":     req.Publish,
		"sponsor":     req.Sponsor,
		"start_time":  req.StartTime,
		"end_time":    req.EndTime,
		"freeze_time": req.FreezeTime,
		"problem_set": string(j),
		"public":      req.Public,
		"code":        req.Code,
	}

	return cs.repo.UpdateContest(ctx, data, c.ID)

}

// GetContestList 获取比赛列表
func (cs *contest) GetContestList(ctx *gin.Context, req *entity.ContestList) ([]*model.Contest, error) {
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
func (cs *contest) GetListByUserID(ctx *gin.Context, id, page, pageSize int) ([]*model.Contest, error) {
	return cs.repo.GetListByUserID(ctx, id, page, pageSize)
}

// GetContestInfoByID 获取比赛详情
func (cs *contest) GetContestInfoByID(ctx *gin.Context, id int) (*model.Contest, error) {
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
func (cs *contest) DeleteContest(ctx *gin.Context, id int) error {
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
