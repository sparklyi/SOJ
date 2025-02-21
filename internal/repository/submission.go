package repository

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/utils"
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type SubmissionRepository interface {
	CreateSubmission(ctx *gin.Context, s *model.Submission) error
	GetInfoByID(ctx *gin.Context, id int) (*model.Submission, error)
	GetSubmissionList(ctx *gin.Context, req *entity.SubmissionList) ([]*model.Submission, error)
	DeleteJudgeByToken(ctx *gin.Context, token string) error
	DeleteAllJudgeHistory(ctx context.Context) error
}

type submission struct {
	log        *zap.Logger
	db         *gorm.DB
	postgresql *gorm.DB
}

func NewSubmissionRepository(log *zap.Logger, db *gorm.DB) SubmissionRepository {
	//连接postgres
	dsn := viper.GetString("postgresql.dsn")
	p, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	return &submission{
		log:        log,
		db:         db,
		postgresql: p,
	}
}

// CreateSubmission 创建测评记录
func (sr *submission) CreateSubmission(ctx *gin.Context, s *model.Submission) error {
	err := sr.db.WithContext(ctx).Create(s).Error
	if err != nil {
		sr.log.Error("创建测评记录失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// GetInfoByID 根据id获取测评记录
func (sr *submission) GetInfoByID(ctx *gin.Context, id int) (*model.Submission, error) {
	var submission model.Submission
	err := sr.db.WithContext(ctx).First(&submission, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(constant.NotFoundError)
	} else if err != nil {
		sr.log.Error("查询数据库失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return &submission, nil
}

// GetSubmissionList 获取测评列表
func (sr *submission) GetSubmissionList(ctx *gin.Context, req *entity.SubmissionList) ([]*model.Submission, error) {
	db := sr.db.WithContext(ctx).Model(&model.Submission{})
	if req.UserID != 0 {
		db = db.Where("user_id = ?", req.UserID)
	}
	if req.UserName != "" {
		db = db.Where("user_name LIKE ?", req.UserName+"%")
	}
	if req.LanguageID != 0 {
		db = db.Where("language_id = ?", req.LanguageID)
	}
	if req.ContestID != 0 {
		db = db.Where("contest_id = ?", req.ContestID)
	}
	if req.ProblemID != 0 {
		db = db.Where("problem_id = ?", req.ProblemID)
	}

	var submissions []*model.Submission
	if err := db.Scopes(utils.Paginate(req.Page, req.PageSize)).Find(&submissions).Error; err != nil {
		sr.log.Error("查询数据库失败", zap.Error(err))
		return nil, err
	}
	return submissions, nil

}

// DeleteJudgeByToken 删除postgresql的测评记录
func (sr *submission) DeleteJudgeByToken(ctx *gin.Context, token string) error {
	err := sr.postgresql.WithContext(ctx).Table("submissions").Where("token = ?", token).Delete(nil).Error
	if err != nil {
		sr.log.Error("删除postgres测评记录失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// DeleteAllJudgeHistory 删除postgres的测评历史
func (sr *submission) DeleteAllJudgeHistory(ctx context.Context) error {
	err := sr.postgresql.WithContext(ctx).Exec("DELETE FROM submissions;").Error
	if err != nil {
		sr.log.Error("删除postgres测评历史失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}
