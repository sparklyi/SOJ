package service

import (
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/repository"
	"context"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type LanguageService struct {
	log  *zap.Logger
	repo *repository.LanguageRepository
}

func NewLanguageService(log *zap.Logger, repo *repository.LanguageRepository) *LanguageService {
	return &LanguageService{
		log:  log,
		repo: repo,
	}
}

// GetLanguageList 获取语言列表
func (ls *LanguageService) GetLanguageList(ctx *gin.Context, req *entity.Language) ([]*model.Language, error) {
	return ls.repo.GetLangList(ctx, req)
}

// UpdateLang 更新语言
func (ls *LanguageService) UpdateLang(ctx *gin.Context, req *entity.Language) error {

	lang := &model.Language{
		ID:     req.ID,
		Name:   req.Name,
		Status: req.Status,
	}
	return ls.repo.Update(ctx, lang)

}

// GetInfo 获取语言信息
func (ls *LanguageService) GetInfo(ctx *gin.Context, id int) (*model.Language, error) {
	return ls.repo.GetByID(ctx, id)

}

// SyncJudge0Lang 同步judge0语言信息
func (ls *LanguageService) SyncJudge0Lang(ctx context.Context) error {
	return ls.repo.SyncLanguages(ctx)
}
