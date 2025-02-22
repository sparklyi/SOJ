package service

import (
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/repository"
	"context"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type LanguageService interface {
	GetLanguageList(ctx *gin.Context, req *entity.Language) ([]*model.Language, error)
	UpdateLang(ctx *gin.Context, req *entity.Language) error
	GetInfo(ctx *gin.Context, id int) (*model.Language, error)
	SyncJudge0Lang(ctx context.Context) error
}

type language struct {
	log  *zap.Logger
	repo repository.LanguageRepository
}

func NewLanguageService(log *zap.Logger, repo repository.LanguageRepository) LanguageService {
	return &language{
		log:  log,
		repo: repo,
	}
}

// GetLanguageList 获取语言列表
func (ls *language) GetLanguageList(ctx *gin.Context, req *entity.Language) ([]*model.Language, error) {
	return ls.repo.GetLangList(ctx, req)
}

// UpdateLang 更新语言
func (ls *language) UpdateLang(ctx *gin.Context, req *entity.Language) error {

	lang := &model.Language{
		ID:     uint(req.ID),
		Name:   req.Name,
		Status: req.Status,
	}
	return ls.repo.Update(ctx, lang)

}

// GetInfo 获取语言信息
func (ls *language) GetInfo(ctx *gin.Context, id int) (*model.Language, error) {
	return ls.repo.GetByID(ctx, id)

}

// SyncJudge0Lang 同步judge0语言信息
func (ls *language) SyncJudge0Lang(ctx context.Context) error {
	return ls.repo.SyncLanguages(ctx)
}
