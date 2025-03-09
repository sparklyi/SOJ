package repository

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/utils"
	"context"
	"encoding/json"
	"errors"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"io"
	"net/http"
	"time"
)

type LanguageRepository interface {
	Create(ctx context.Context, lang *model.Language) (*model.Language, error)
	Update(ctx context.Context, lang *model.Language) error
	Delete(ctx context.Context, id int) error
	GetByID(ctx context.Context, id int) (*model.Language, error)
	GetLangList(ctx context.Context, req *entity.Language) ([]*model.Language, error)
	Count(ctx context.Context) (int64, error)
	GetTransaction(ctx context.Context) *gorm.DB
	SyncLanguages(ctx context.Context) error
}
type language struct {
	log    *zap.Logger
	db     *gorm.DB
	client *http.Client
}

func NewLanguageRepository(log *zap.Logger, db *gorm.DB) LanguageRepository {
	return &language{
		log:    log,
		db:     db,
		client: &http.Client{Timeout: time.Second * 10},
	}
}

// Create 语言增加
func (lr *language) Create(ctx context.Context, lang *model.Language) (*model.Language, error) {
	err := lr.db.WithContext(ctx).Create(lang).Error
	if err != nil {
		lr.log.Error("create language error", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return lang, nil
}

// Update 语言更新(名字 是否可用)
func (lr *language) Update(ctx context.Context, lang *model.Language) error {
	err := lr.db.Debug().WithContext(ctx).Updates(lang).Error
	if err != nil {
		lr.log.Error("update language error", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// Delete 删除语言(不可用 语言删除只能手动去docker环境)
func (lr *language) Delete(ctx context.Context, id int) error {
	err := lr.db.WithContext(ctx).Delete(&model.Language{}, id).Error
	if err != nil {
		lr.log.Error("delete language error", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// GetByID 根据id获取语言信息
func (lr *language) GetByID(ctx context.Context, id int) (*model.Language, error) {
	var lang model.Language
	err := lr.db.WithContext(ctx).First(&lang, id).Error
	if err != nil {
		lr.log.Error("get language error", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return &lang, nil
}

// GetLangList 获取语言列表
func (lr *language) GetLangList(ctx context.Context, req *entity.Language) ([]*model.Language, error) {
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
func (lr *language) Count(ctx context.Context) (int64, error) {
	var count int64
	err := lr.db.WithContext(ctx).Model(&model.Language{}).Count(&count).Error
	if err != nil {
		lr.log.Error("count language error", zap.Error(err))
		return -1, errors.New(constant.ServerError)
	}
	return count, nil
}

// GetTransaction 获取事务
func (lr *language) GetTransaction(ctx context.Context) *gorm.DB {
	return lr.db.WithContext(ctx).Begin()
}

// SyncLanguages 测评语言同步
func (lr *language) SyncLanguages(ctx context.Context) error {

	url := viper.GetString("codenire.url") + "/actions"

	resp, err := lr.client.Get(url)
	if err != nil || resp.Status != "200 OK" {
		lr.log.Error("同步测评语言失败:", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	var respData []entity.LanguageSync
	if err = json.Unmarshal(data, &respData); err != nil {
		lr.log.Error("解析沙箱响应失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	//会出现几个查看版本的命令
	//因为语言数量不会很多 没必要设置索引 直接走全表查询

	for _, v := range respData {
		var lang model.Language
		err = lr.db.WithContext(ctx).
			Model(&model.Language{}).
			Where("action_id = ? and name = ? and template = ?", v.Id, v.Name, v.Template).
			First(&lang).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			lr.log.Error("查询数据库失败", zap.Error(err))
			return errors.New(constant.ServerError)
		}
		if lang.ID != 0 {
			continue
		}
		lang.ActionID = v.Id
		lang.Name = v.Name
		lang.Template = v.Template
		err = lr.db.WithContext(ctx).Create(&lang).Error
		if err != nil {
			lr.log.Error("新增测评语言失败", zap.Error(err))
			return errors.New(constant.ServerError)
		}

	}
	return nil
}
