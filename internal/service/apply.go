package service

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/repository"
	"SOJ/utils"
	"errors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"time"
)

type ApplyService struct {
	log         *zap.Logger
	repo        *repository.ApplyRepository
	contestRepo *repository.ContestRepository
}

func NewApplyService(log *zap.Logger, repo *repository.ApplyRepository, c *repository.ContestRepository) *ApplyService {
	return &ApplyService{
		log:         log,
		repo:        repo,
		contestRepo: c,
	}
}

// CreateApply 创建报名
func (as *ApplyService) CreateApply(ctx *gin.Context, req *entity.Apply) (*model.Apply, error) {

	claims := utils.GetAccessClaims(ctx)

	apply := &model.Apply{
		UserID:    uint(claims.ID),
		ContestID: req.ContestID,
		Name:      req.Name,
	}
	a, err := as.repo.GetInfoByUserAndContest(ctx, apply.UserID, apply.ContestID)
	//已经报名
	if a != nil && err == nil {
		return nil, errors.New(constant.AlreadyExistError)
	} else if err != nil && err.Error() != constant.NotFoundError {
		return nil, err
	}
	//比赛已结束或不存在
	c, err := as.contestRepo.GetContestInfoByID(ctx, int(apply.ContestID))
	if err != nil {
		return nil, err
	}
	if c.EndTime.Unix() < time.Now().Unix() {
		return nil, errors.New(constant.DisableError)
	}
	err = as.repo.CreateApply(ctx, apply)
	if err != nil {
		return nil, err
	}
	return apply, nil

}

// UpdateApply 更新报名
func (as *ApplyService) UpdateApply(ctx *gin.Context, req *entity.Apply) error {

	apply, err := as.repo.GetInfoByID(ctx, req.ID)
	if err != nil {
		return err
	}
	claims := utils.GetAccessClaims(ctx)
	if apply.UserID != uint(claims.ID) && claims.Auth < constant.AdminLevel {
		return errors.New(constant.UnauthorizedError)
	}
	if req.Name != "" {
		apply.Name = req.Name
	}
	return as.repo.UpdateApply(ctx, apply)

}

// DeleteApply 取消报名
func (as *ApplyService) DeleteApply(ctx *gin.Context, aid int) error {
	claims := utils.GetAccessClaims(ctx)
	apply, err := as.repo.GetInfoByID(ctx, aid)
	if err != nil {
		return err
	}
	//非本人且非管理员
	if apply.UserID != uint(claims.ID) && claims.Auth < constant.AdminLevel {
		return errors.New(constant.UnauthorizedError)
	}
	return as.repo.DeleteApply(ctx, aid)

}

// GetListByUserID 获取用户报名信息
func (as *ApplyService) GetListByUserID(ctx *gin.Context, page, pageSize int) ([]*model.Apply, error) {
	claims := utils.GetAccessClaims(ctx)
	return as.repo.GetListByUserID(ctx, claims.ID, page, pageSize)
}

// GetList 获取报名列表
func (as *ApplyService) GetList(ctx *gin.Context, req *entity.ApplyList) ([]*model.Apply, error) {
	return as.repo.GetList(ctx, req)
}

// GetInfoByID 根据报名id获取详情
func (as *ApplyService) GetInfoByID(ctx *gin.Context, aid int) (*model.Apply, error) {
	return as.repo.GetInfoByID(ctx, aid)
}

// GetInfoByUserAndContest 根据用户id和比赛id获取报名详情
func (as *ApplyService) GetInfoByUserAndContest(ctx *gin.Context, uid, cid uint) (*model.Apply, error) {
	return as.repo.GetInfoByUserAndContest(ctx, uid, cid)
}
