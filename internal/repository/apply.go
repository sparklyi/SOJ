package repository

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/utils"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type ApplyRepository interface {
	CreateApply(ctx *gin.Context, apply *model.Apply) error
	UpdateApply(ctx *gin.Context, apply *model.Apply) error
	DeleteApply(ctx *gin.Context, aid int) error
	GetListByUserID(ctx *gin.Context, uid int, page int, pageSize int) ([]*model.Apply, error)
	GetList(ctx *gin.Context, req *entity.ApplyList) ([]*model.Apply, error)
	GetInfoByUserAndContest(ctx *gin.Context, uid uint, cid uint) (*model.Apply, error)
	GetInfoByID(ctx *gin.Context, id int) (*model.Apply, error)
	DeleteApplyByContestID(ctx *gin.Context, cid int) error
}

type apply struct {
	log *zap.Logger
	db  *gorm.DB
}

func NewApplyRepository(log *zap.Logger, db *gorm.DB) ApplyRepository {
	return &apply{
		log: log,
		db:  db,
	}
}

// CreateApply 创建报名
func (ar *apply) CreateApply(ctx *gin.Context, apply *model.Apply) error {
	err := ar.db.WithContext(ctx).Create(apply).Error
	if err != nil {
		ar.log.Error("报名失败", zap.Error(err), zap.Any("apply info", apply))
		return errors.New(constant.ServerError)
	}
	return nil
}

// UpdateApply 更新报名
func (ar *apply) UpdateApply(ctx *gin.Context, apply *model.Apply) error {
	err := ar.db.WithContext(ctx).Save(apply).Error
	if err != nil {
		ar.log.Error("报名信息更新失败", zap.Error(err), zap.Any("apply info", apply))
		return errors.New(constant.ServerError)
	}
	return nil
}

// DeleteApply 取消报名
func (ar *apply) DeleteApply(ctx *gin.Context, aid int) error {
	err := ar.db.WithContext(ctx).Delete(&model.Apply{}, aid).Error
	if err != nil {
		ar.log.Error("取消报名失败", zap.Error(err), zap.Any("apply id", aid))
		return errors.New(constant.ServerError)
	}
	return nil
}

// GetListByUserID 获取用户报名信息
func (ar *apply) GetListByUserID(ctx *gin.Context, uid int, page, pageSize int) ([]*model.Apply, error) {
	var a []*model.Apply
	err := ar.db.WithContext(ctx).Scopes(utils.Paginate(page, pageSize)).Where("user_id = ?", uid).Find(&a).Error
	if err != nil {
		ar.log.Error("获取报名信息失败", zap.Error(err), zap.Any("user_id", uid))
		return nil, errors.New(constant.ServerError)
	}
	return a, nil
}

// GetList 获取报名列表
func (ar *apply) GetList(ctx *gin.Context, req *entity.ApplyList) ([]*model.Apply, error) {
	db := ar.db.WithContext(ctx)
	if req.ID != 0 {
		db = db.Where("id = ?", req.ID)
	}
	if req.UserID != 0 {
		db = db.Where("user_id = ?", req.UserID)
	}
	if req.ContestID != 0 {
		db = db.Where("contest_id = ?", req.ContestID)
	}
	var a []*model.Apply
	err := db.Scopes(utils.Paginate(req.Page, req.PageSize)).Find(&a).Error
	if err != nil {
		ar.log.Error("查询报名列表失败", zap.Error(err), zap.Any("search info", req))
		return nil, errors.New(constant.ServerError)
	}
	return a, nil

}

// GetInfoByUserAndContest 根据用户id和比赛id获取报名详情
func (ar *apply) GetInfoByUserAndContest(ctx *gin.Context, uid, cid uint) (*model.Apply, error) {
	a := &model.Apply{}
	err := ar.db.WithContext(ctx).Where("user_id = ? AND contest_id = ?", uid, cid).First(a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(constant.NotFoundError)
	} else if err != nil {
		ar.log.Error("获取报名详情失败", zap.Error(err), zap.Any("info", fmt.Sprintf("uid:%v,cid:%v", uid, cid)))
		return nil, errors.New(constant.ServerError)
	}
	return a, nil
}

// GetInfoByID 根据报名id获取详情
func (ar *apply) GetInfoByID(ctx *gin.Context, id int) (*model.Apply, error) {
	a := &model.Apply{}
	err := ar.db.WithContext(ctx).Where("id = ?", id).First(a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(constant.NotFoundError)
	} else if err != nil {
		ar.log.Error("获取报名详情", zap.Error(err), zap.Any("id", id))
		return nil, errors.New(constant.ServerError)
	}
	return a, nil
}

// DeleteApplyByContestID 删除所有cid的报名信息
func (ar *apply) DeleteApplyByContestID(ctx *gin.Context, cid int) error {
	err := ar.db.WithContext(ctx).Where("contest_id = ?", cid).Delete(&model.Apply{}).Error
	if err != nil {
		ar.log.Error("删除记录失败", zap.Error(err), zap.Any("contest_id", cid))
		return errors.New(constant.ServerError)
	}
	return nil
}
