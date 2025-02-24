package repository

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/utils"
	"errors"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type ContestRepository interface {
	CreateContest(ctx *gin.Context, contest *model.Contest) (*model.Contest, error)
	GetContestInfoByID(ctx *gin.Context, id int) (*model.Contest, error)
	UpdateContest(ctx *gin.Context, data map[string]interface{}, id uint) error
	GetContestList(ctx *gin.Context, req *entity.ContestList, admin bool) ([]*model.Contest, error)
	DeleteContest(ctx *gin.Context, id int) error
	GetListByUserID(ctx *gin.Context, uid int, page int, pageSize int) ([]*model.Contest, error)
}

type contest struct {
	log         *zap.Logger
	db          *gorm.DB
	ContestColl *mongo.Collection
}

func NewContestRepository(log *zap.Logger, db *gorm.DB, m *mongo.Database) ContestRepository {
	return &contest{
		log:         log,
		db:          db,
		ContestColl: m.Collection("contest"),
	}
}

// CreateContest 比赛创建
func (cr *contest) CreateContest(ctx *gin.Context, contest *model.Contest) (*model.Contest, error) {
	err := cr.db.WithContext(ctx).Create(contest).Error
	if err != nil {
		cr.log.Error("比赛创建失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return contest, nil
}

// GetContestInfoByID 获取比赛信息
func (cr *contest) GetContestInfoByID(ctx *gin.Context, id int) (*model.Contest, error) {
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
func (cr *contest) UpdateContest(ctx *gin.Context, data map[string]interface{}, id uint) error {
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
func (cr *contest) GetContestList(ctx *gin.Context, req *entity.ContestList, admin bool) ([]*model.Contest, error) {
	db := cr.db.WithContext(ctx).Model(&model.Contest{})
	if !admin {
		req.Publish = new(bool)
		*req.Publish = true
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
	if req.Publish != nil {
		db = db.Where("publish = ?", req.Publish)
	}
	var list []*model.Contest
	err := db.Scopes(utils.Paginate(req.Page, req.PageSize)).Find(&list).Error
	if err != nil {
		cr.log.Error("获取比赛列表失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return list, nil

}

// DeleteContest 删除比赛
func (cr *contest) DeleteContest(ctx *gin.Context, id int) error {
	err := cr.db.WithContext(ctx).Delete(&model.Contest{}, id).Error
	if err != nil {
		cr.log.Error("删除比赛失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// GetListByUserID 获取用户创建的比赛
func (cr *contest) GetListByUserID(ctx *gin.Context, uid int, page int, pageSize int) ([]*model.Contest, error) {
	var list []*model.Contest
	err := cr.db.WithContext(ctx).Scopes(utils.Paginate(page, pageSize)).Find(&list).Error
	if err != nil {
		cr.log.Error("获取用户比赛列表失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return list, nil
}
