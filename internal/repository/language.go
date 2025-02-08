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

type LanguageRepository struct {
	log        *zap.Logger
	db         *gorm.DB
	postgresql *gorm.DB
}

func NewLanguageRepository(log *zap.Logger, db *gorm.DB) *LanguageRepository {
	//连接postgres
	dsn := viper.GetString("postgresql.dsn")
	p, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	return &LanguageRepository{
		log:        log,
		db:         db,
		postgresql: p,
	}
}

func (lr *LanguageRepository) Create(ctx *gin.Context, lang *model.Language) (*model.Language, error) {
	err := lr.db.WithContext(ctx).Create(lang).Error
	if err != nil {
		lr.log.Error("create language error", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return lang, nil
}

func (lr *LanguageRepository) Update(ctx *gin.Context, lang *model.Language) error {
	err := lr.db.WithContext(ctx).Save(lang).Error
	if err != nil {
		lr.log.Error("update language error", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

func (lr *LanguageRepository) Delete(ctx *gin.Context, id int) error {
	err := lr.db.WithContext(ctx).Delete(&model.Language{}, id).Error
	if err != nil {
		lr.log.Error("delete language error", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

func (lr *LanguageRepository) GetByID(ctx *gin.Context, id int) (*model.Language, error) {
	var lang model.Language
	err := lr.db.WithContext(ctx).First(&lang, id).Error
	if err != nil {
		lr.log.Error("get language error", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return &lang, nil
}

func (lr *LanguageRepository) GetLangList(ctx *gin.Context, req *entity.Language) ([]*model.Language, error) {
	var langs []*model.Language
	db := lr.db.WithContext(ctx).Model(&model.Language{})
	if req.Name != "" {
		db = db.Where("name LIKE ?", "%"+req.Name+"%")
	}
	if req.ID != 0 {
		db = db.Where("id = ?", req.ID)
	}
	if req.Status != nil {
		db = db.Where("status = ?", *req.Status)
	}

	err := db.Scopes(utils.Paginate(req.Page, req.PageSize)).Find(&langs).Error
	if err != nil {
		lr.log.Error("get language error", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return langs, nil
}

func (lr *LanguageRepository) Count(ctx *gin.Context) (int64, error) {
	var count int64
	err := lr.db.WithContext(ctx).Model(&model.Language{}).Count(&count).Error
	if err != nil {
		lr.log.Error("count language error", zap.Error(err))
		return -1, errors.New(constant.ServerError)
	}
	return count, nil
}
func (lr *LanguageRepository) GetTransaction(ctx *gin.Context) *gorm.DB {
	return lr.db.WithContext(ctx).Begin()
}

// SyncLanguages 测评语言同步
func (lr *LanguageRepository) SyncLanguages(ctx context.Context) error {
	lang := make([]*model.Language, 0)
	err := lr.postgresql.Table("languages").Where("is_Archived = false").Find(&lang).Error
	if err != nil {
		lr.log.Error("测评语言同步失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	//status列不同步，由管理员手动调整
	err = lr.db.WithContext(ctx).Omit("status").Save(&lang).Error
	if err != nil {
		lr.log.Error("测评语言同步失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}
