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

type ApplyService interface {
	CreateApply(ctx *gin.Context, req *entity.Apply) (*model.Apply, error)
	UpdateApply(ctx *gin.Context, req *entity.Apply) error
	DeleteApply(ctx *gin.Context, aid int) error
	GetListByUserID(ctx *gin.Context, page int, pageSize int) ([]*model.Apply, error)
	GetList(ctx *gin.Context, req *entity.ApplyList) ([]*model.Apply, error)
	GetInfoByID(ctx *gin.Context, aid int) (*model.Apply, error)
	GetInfoByUserAndContest(ctx *gin.Context, uid uint, cid uint) (*model.Apply, error)
}

type apply struct {
	log         *zap.Logger
	repo        repository.ApplyRepository
	contestRepo repository.ContestRepository
}

func NewApplyService(log *zap.Logger, repo repository.ApplyRepository, c repository.ContestRepository) ApplyService {
	return &apply{
		log:         log,
		repo:        repo,
		contestRepo: c,
	}
}

// CreateApply 创建报名
func (as *apply) CreateApply(ctx *gin.Context, req *entity.Apply) (*model.Apply, error) {

	claims := utils.GetAccessClaims(ctx)

	a := &model.Apply{
		UserID:    uint(claims.ID),
		ContestID: req.ContestID,
		Name:      req.Name,
		Email:     req.Email,
	}
	applyResp, err := as.repo.GetInfoByUserAndContest(ctx, a.UserID, a.ContestID)
	//已经报名Resp
	if applyResp != nil && err == nil {
		return nil, errors.New(constant.AlreadyExistError)
	} else if err != nil && err.Error() != constant.NotFoundError {
		return nil, err
	}
	//比赛已结束或不存在
	c, err := as.contestRepo.GetContestInfoByID(ctx, int(a.ContestID))
	if err != nil {
		return nil, err
	}
	if c.EndTime.Unix() < time.Now().Unix() || *c.Publish == false {
		return nil, errors.New(constant.DisableError)
	}

	//私有比赛检查code
	if !*c.Public && c.Code != req.Code {
		return nil, errors.New(constant.CodeError)
	}
	err = as.repo.CreateApply(ctx, a)
	if err != nil {
		return nil, err
	}
	return a, nil

}

// UpdateApply 更新报名
func (as *apply) UpdateApply(ctx *gin.Context, req *entity.Apply) error {

	applyResp, err := as.repo.GetInfoByID(ctx, req.ID)
	if err != nil {
		return err
	}
	claims := utils.GetAccessClaims(ctx)
	if applyResp.UserID != uint(claims.ID) && claims.Auth < constant.AdminLevel {
		return errors.New(constant.UnauthorizedError)
	}
	applyResp.Name = req.Name
	applyResp.Email = req.Email
	return as.repo.UpdateApply(ctx, applyResp, nil)

}

// DeleteApply 取消报名
func (as *apply) DeleteApply(ctx *gin.Context, aid int) error {
	claims := utils.GetAccessClaims(ctx)
	applyResp, err := as.repo.GetInfoByID(ctx, aid)
	if err != nil {
		return err
	}
	//非本人且非管理员
	if applyResp.UserID != uint(claims.ID) && claims.Auth < constant.AdminLevel {
		return errors.New(constant.UnauthorizedError)
	}
	return as.repo.DeleteApply(ctx, aid)

}

// GetListByUserID 获取用户报名信息
func (as *apply) GetListByUserID(ctx *gin.Context, page, pageSize int) ([]*model.Apply, error) {
	claims := utils.GetAccessClaims(ctx)
	return as.repo.GetListByUserID(ctx, claims.ID, page, pageSize)
}

// GetList 获取报名列表
func (as *apply) GetList(ctx *gin.Context, req *entity.ApplyList) ([]*model.Apply, error) {
	return as.repo.GetList(ctx, req)
}

// GetInfoByID 根据报名id获取详情
func (as *apply) GetInfoByID(ctx *gin.Context, aid int) (*model.Apply, error) {
	return as.repo.GetInfoByID(ctx, aid)
}

// GetInfoByUserAndContest 根据用户id和比赛id获取报名详情
func (as *apply) GetInfoByUserAndContest(ctx *gin.Context, uid, cid uint) (*model.Apply, error) {
	return as.repo.GetInfoByUserAndContest(ctx, uid, cid)
}
