package repository

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/utils"
	"context"
	"errors"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"time"
)

type ContestRepository interface {
	CreateContest(ctx context.Context, contest *model.Contest) (*model.Contest, error)
	GetContestInfoByID(ctx context.Context, id int) (*model.Contest, error)
	UpdateContest(ctx context.Context, data map[string]interface{}, id uint) error
	GetContestList(ctx context.Context, req *entity.ContestList, admin bool) ([]*model.Contest, int64, error)
	DeleteContest(ctx context.Context, id int) error
	GetListByUserID(ctx context.Context, uid int, page int, pageSize int) ([]*model.Contest, int64, error)
}

type contest struct {
	log *zap.Logger
	db  *gorm.DB
}

func NewContestRepository(log *zap.Logger, db *gorm.DB) ContestRepository {
	return &contest{
		log: log,
		db:  db,
	}
}

// CreateContest 比赛创建
func (cr *contest) CreateContest(ctx context.Context, contest *model.Contest) (*model.Contest, error) {
	err := cr.db.WithContext(ctx).Create(contest).Error
	if err != nil {
		cr.log.Error("比赛创建失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return contest, nil
}

// GetContestInfoByID 获取比赛信息
func (cr *contest) GetContestInfoByID(ctx context.Context, id int) (*model.Contest, error) {
	c := &model.Contest{}
	err := cr.db.WithContext(ctx).First(c, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(constant.NotFoundError)

	} else if err != nil {
		cr.log.Error("获取数据库信息失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return c, nil
}

// UpdateContest 更新比赛
func (cr *contest) UpdateContest(ctx context.Context, data map[string]interface{}, id uint) error {
	err := cr.db.WithContext(ctx).
		Model(&model.Contest{}).
		Where("id = ?", id).
		Updates(data).Error
	if err != nil {
		cr.log.Error("比赛更新失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// GetContestList 获取比赛列表
func (cr *contest) GetContestList(ctx context.Context, req *entity.ContestList, admin bool) ([]*model.Contest, int64, error) {
	db := cr.db.WithContext(ctx).Model(&model.Contest{})
	if !admin {
		req.Publish = new(bool)
		*req.Publish = true
		db = db.Where("end_time >= ?", time.Now())
	}
	if req.ID != 0 {
		db = db.Where("id = ?", req.ID)
	}
	if req.Tag != "" {
		db = db.Where("tag = ?", req.Tag)
	}
	if req.Type != "" {
		db = db.Where("type = ?", req.Type)
	}
	if req.Public != nil {
		db = db.Where("public = ?", req.Public)
	}
	if req.UserID != 0 {
		db = db.Where("user_id = ?", req.UserID)
	}

	var count int64
	err := db.Count(&count).Error
	if err != nil {
		cr.log.Error("数据库查询失败", zap.Error(err))
		return nil, 0, errors.New(constant.ServerError)
	}
	var list []*model.Contest
	err = db.Scopes(utils.Paginate(req.Page, req.PageSize)).Find(&list).Error
	if err != nil {
		cr.log.Error("获取比赛列表失败", zap.Error(err))
		return nil, 0, errors.New(constant.ServerError)
	}
	return list, count, nil

}

// DeleteContest 删除比赛
func (cr *contest) DeleteContest(ctx context.Context, id int) error {
	err := cr.db.WithContext(ctx).Delete(&model.Contest{}, id).Error
	if err != nil {
		cr.log.Error("删除比赛失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// GetListByUserID 获取用户创建的比赛
func (cr *contest) GetListByUserID(ctx context.Context, uid int, page int, pageSize int) ([]*model.Contest, int64, error) {

	var count int64
	err := cr.db.Model(&model.Contest{}).Count(&count).Error
	if err != nil {
		cr.log.Error("数据库查询失败", zap.Error(err))
		return nil, 0, errors.New(constant.ServerError)
	}
	var list []*model.Contest
	err = cr.db.WithContext(ctx).Scopes(utils.Paginate(page, pageSize)).Where("id = ?", uid).Find(&list).Error
	if err != nil {
		cr.log.Error("获取用户比赛列表失败", zap.Error(err))
		return nil, 0, errors.New(constant.ServerError)
	}
	return list, count, nil
}
