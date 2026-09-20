package submission

import (
	"context"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/judge"
)

type languageStore interface {
	ListLanguages(context.Context, ListLanguagesInput) ([]LanguageRecord, int64, error)
	UpsertLanguage(context.Context, judge.Language) (LanguageRecord, error)
	UpdateLanguage(context.Context, int64, UpdateLanguageInput) (LanguageRecord, error)
}

// LanguageService owns language catalog administration and public queries.
type LanguageService struct {
	store    languageStore
	provider languageProvider
	stats    statsRefresher
}

// NewLanguageService builds the service. 最后一个可选参数是站级聚合刷新器
// （见 stats 包）：语言目录写入 PG 成功后刷新首页聚合缓存。
func NewLanguageService(store languageStore, provider languageProvider, stats ...statsRefresher) *LanguageService {
	service := &LanguageService{store: store, provider: provider}
	if len(stats) > 0 {
		service.stats = stats[0]
	}
	return service
}

func (s *LanguageService) ListLanguages(ctx context.Context, actor auth.Actor, input ListLanguagesInput) ([]LanguageRecord, int64, error) {
	if !actor.Admin() {
		return nil, 0, apperror.Forbidden("admin_required", "admin role required")
	}
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 50
	}
	return s.store.ListLanguages(ctx, input)
}

func (s *LanguageService) ListPublicLanguages(ctx context.Context, _ auth.Actor, input ListLanguagesInput) ([]LanguageRecord, int64, error) {
	enabled := true
	input.Enabled = &enabled
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 50
	}
	return s.store.ListLanguages(ctx, input)
}

func (s *LanguageService) SyncLanguages(ctx context.Context, actor auth.Actor) ([]LanguageRecord, error) {
	if !actor.Root() {
		return nil, apperror.Forbidden("root_required", "root role required")
	}
	languages, err := s.provider.Languages(ctx)
	if err != nil {
		return nil, err
	}
	updated := make([]LanguageRecord, 0, len(languages))
	for _, language := range languages {
		record, err := s.store.UpsertLanguage(ctx, language)
		if err != nil {
			return nil, err
		}
		updated = append(updated, record)
	}
	s.refreshStats(ctx)
	return updated, nil
}

func (s *LanguageService) UpdateLanguage(ctx context.Context, actor auth.Actor, id int64, input UpdateLanguageInput) (LanguageRecord, error) {
	if !actor.Admin() {
		return LanguageRecord{}, apperror.Forbidden("admin_required", "admin role required")
	}
	record, err := s.store.UpdateLanguage(ctx, id, input)
	if err == nil {
		// 启用/停用会改变公开语言数，首页聚合跟着走。
		s.refreshStats(ctx)
	}
	return record, err
}

func (s *LanguageService) refreshStats(ctx context.Context) {
	if s.stats != nil {
		s.stats.Refresh(ctx)
	}
}
