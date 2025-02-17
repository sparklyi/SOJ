package repository

import (
	"SOJ/internal/constant"
	"SOJ/internal/model"
	"errors"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type ContestRepository struct {
	log         *zap.Logger
	db          *gorm.DB
	ContestColl *mongo.Collection
}

func NewContestRepository(log *zap.Logger, db *gorm.DB, m *mongo.Database) *ContestRepository {
	return &ContestRepository{
		log:         log,
		db:          db,
		ContestColl: m.Collection("contest"),
	}
}

// CreateContest 比赛创建
func (cr *ContestRepository) CreateContest(ctx *gin.Context, contest *model.Contest) (*model.Contest, error) {
	err := cr.db.WithContext(ctx).Create(contest).Error
	if err != nil {
		cr.log.Error("比赛创建失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return contest, nil
}
func (cr *ContestRepository) UpdateContest(ctx *gin.Context, contest *model.Contest) error {
	err := cr.db.WithContext(ctx).Save(contest).Error
	if err != nil {
		cr.log.Error("比赛更新失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}
