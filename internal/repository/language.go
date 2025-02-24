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

type LanguageRepository interface {
	Create(ctx *gin.Context, lang *model.Language) (*model.Language, error)
	Update(ctx *gin.Context, lang *model.Language) error
	Delete(ctx *gin.Context, id int) error
	GetByID(ctx *gin.Context, id int) (*model.Language, error)
	GetLangList(ctx *gin.Context, req *entity.Language) ([]*model.Language, error)
	Count(ctx *gin.Context) (int64, error)
	GetTransaction(ctx *gin.Context) *gorm.DB
	SyncLanguages(ctx context.Context) error
}
type language struct {
	log        *zap.Logger
	db         *gorm.DB
	postgresql *gorm.DB
}

func NewLanguageRepository(log *zap.Logger, db *gorm.DB) LanguageRepository {
	//连接postgres
	dsn := viper.GetString("postgresql.dsn")
	p, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	return &language{
		log:        log,
		db:         db,
		postgresql: p,
	}
}

// Create 语言增加
func (lr *language) Create(ctx *gin.Context, lang *model.Language) (*model.Language, error) {
	err := lr.db.WithContext(ctx).Create(lang).Error
	if err != nil {
		lr.log.Error("create language error", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return lang, nil
}

// Update 语言更新(名字 是否可用)
func (lr *language) Update(ctx *gin.Context, lang *model.Language) error {
	err := lr.db.Debug().WithContext(ctx).Updates(lang).Error
	if err != nil {
		lr.log.Error("update language error", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// Delete 删除语言(不可用 语言删除只能手动去docker环境)
func (lr *language) Delete(ctx *gin.Context, id int) error {
	err := lr.db.WithContext(ctx).Delete(&model.Language{}, id).Error
	if err != nil {
		lr.log.Error("delete language error", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// GetByID 根据id获取语言信息
func (lr *language) GetByID(ctx *gin.Context, id int) (*model.Language, error) {
	var lang model.Language
	err := lr.db.WithContext(ctx).First(&lang, id).Error
	if err != nil {
		lr.log.Error("get language error", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return &lang, nil
}

// GetLangList 获取语言列表
func (lr *language) GetLangList(ctx *gin.Context, req *entity.Language) ([]*model.Language, error) {
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

// Count 测评语言统计
func (lr *language) Count(ctx *gin.Context) (int64, error) {
	var count int64
	err := lr.db.WithContext(ctx).Model(&model.Language{}).Count(&count).Error
	if err != nil {
		lr.log.Error("count language error", zap.Error(err))
		return -1, errors.New(constant.ServerError)
	}
	return count, nil
}

// GetTransaction 获取事务
func (lr *language) GetTransaction(ctx *gin.Context) *gorm.DB {
	return lr.db.WithContext(ctx).Begin()
}

// SyncLanguages 测评语言同步
func (lr *language) SyncLanguages(ctx context.Context) error {
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
