package repository

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/utils"
	"context"
	"errors"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type SubmissionRepository interface {
	CreateSubmission(ctx context.Context, s *model.Submission, tx *gorm.DB) error
	GetInfoByID(ctx context.Context, id int) (*model.Submission, error)
	GetSubmissionList(ctx context.Context, req *entity.SubmissionList) ([]*model.Submission, int64, error)
	DeleteJudgeByToken(ctx context.Context, token string) error
	DeleteAllJudgeHistory(ctx context.Context) error
	GetTransaction(ctx context.Context) *gorm.DB
	GetJudgeRankByProblemID(ctx context.Context, problemID int, page, pageSize int) ([]model.Submission, error)
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
func (sr *submission) CreateSubmission(ctx context.Context, s *model.Submission, tx *gorm.DB) error {
	if tx == nil {
		tx = sr.db.WithContext(ctx)
	}
	err := tx.Create(s).Error
	if err != nil {
		sr.log.Error("创建测评记录失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// GetInfoByID 根据id获取测评记录
func (sr *submission) GetInfoByID(ctx context.Context, id int) (*model.Submission, error) {
	var s model.Submission
	err := sr.db.WithContext(ctx).First(&s, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(constant.NotFoundError)
	} else if err != nil {
		sr.log.Error("查询数据库失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return &s, nil
}

// GetSubmissionList 获取测评列表
func (sr *submission) GetSubmissionList(ctx context.Context, req *entity.SubmissionList) ([]*model.Submission, int64, error) {
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

	var count int64
	err := db.Count(&count).Error
	if err != nil {
		sr.log.Error("数据库查询失败", zap.Error(err))
		return nil, 0, errors.New(constant.ServerError)
	}

	var submissions []*model.Submission
	if err = db.Scopes(utils.Paginate(req.Page, req.PageSize)).Order("id DESC").Find(&submissions).Error; err != nil {
		sr.log.Error("查询数据库失败", zap.Error(err))
		return nil, 0, err
	}
	return submissions, count, nil

}

// DeleteJudgeByToken 删除postgresql的测评记录
func (sr *submission) DeleteJudgeByToken(ctx context.Context, token string) error {
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

// GetTransaction 获取事务
func (sr *submission) GetTransaction(ctx context.Context) *gorm.DB {
	return sr.db.Where(ctx).Begin()
}

// GetJudgeRankByProblemID 获取题目时空排行
func (sr *submission) GetJudgeRankByProblemID(ctx context.Context, problemID int, page, pageSize int) ([]model.Submission, error) {
	var data []model.Submission
	err := sr.db.WithContext(ctx).
		Model(&model.Submission{}).
		Scopes(utils.Paginate(page, pageSize)).
		Where("problem_id = ?", problemID).
		Where("status = ? and visible = true", constant.JudgeCode2Details[constant.JudgeAC]).
		Order("time, memory").Find(&data).Error
	if err != nil {
		sr.log.Error("获取题目时空排行失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return data, nil
}
