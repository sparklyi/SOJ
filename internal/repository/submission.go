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
)

type SubmissionRepository interface {
	CreateSubmission(ctx context.Context, s *model.Submission, tx *gorm.DB) error
	GetInfoByID(ctx context.Context, id int) (*model.Submission, error)
	GetSubmissionList(ctx context.Context, req *entity.SubmissionList) ([]*model.Submission, error)
	GetTransaction(ctx context.Context) *gorm.DB
}

type submission struct {
	log *zap.Logger
	db  *gorm.DB
}

func NewSubmissionRepository(log *zap.Logger, db *gorm.DB) SubmissionRepository {

	return &submission{
		log: log,
		db:  db,
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
func (sr *submission) GetSubmissionList(ctx context.Context, req *entity.SubmissionList) ([]*model.Submission, error) {
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

// GetTransaction 获取事务
func (sr *submission) GetTransaction(ctx context.Context) *gorm.DB {
	return sr.db.Where(ctx).Begin()
}
