package submission

import (
	"context"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/authz"
)

type languageStore interface {
	ListLanguages(context.Context, ListLanguagesInput) ([]LanguageRecord, int64, error)
	UpdateLanguage(context.Context, int64, UpdateLanguageInput, int64) (LanguageRecord, error)
}

// LanguageService owns language catalog administration and public queries.
type LanguageService struct {
	store languageStore
	stats statsRefresher
}

// NewLanguageService builds the service. 最后一个可选参数是站级聚合刷新器
// （见 stats 包）：语言目录写入 PG 成功后刷新首页聚合缓存。
func NewLanguageService(store languageStore, stats ...statsRefresher) *LanguageService {
	service := &LanguageService{store: store}
	if len(stats) > 0 {
		service.stats = stats[0]
	}
	return service
}

func (s *LanguageService) ListLanguages(ctx context.Context, actor auth.Actor, input ListLanguagesInput) ([]LanguageRecord, int64, error) {
	if err := authorizeLanguageAdmin(actor); err != nil {
		return nil, 0, err
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

func (s *LanguageService) UpdateLanguage(ctx context.Context, actor auth.Actor, id int64, input UpdateLanguageInput) (LanguageRecord, error) {
	if err := authorizeLanguageAdmin(actor); err != nil {
		return LanguageRecord{}, err
	}
	record, err := s.store.UpdateLanguage(ctx, id, input, actor.UserID)
	if err == nil {
		// 启用/停用会改变公开语言数，首页聚合跟着走。
		s.refreshStats(ctx)
	}
	return record, err
}

// authorizeLanguageAdmin keeps the language catalog behind the reserved system
// permission instead of the admin role, so a future system role can hold it
// without becoming a full administrator.
func authorizeLanguageAdmin(actor auth.Actor) error {
	if err := authz.Authorize(authz.NewSubject(actor), authz.PermissionSystemManage); err != nil {
		return apperror.Forbidden("forbidden", "required permission is missing")
	}
	return nil
}

// requireLanguageEnabled is the shared admission rule for writes that name a
// language. The store returns the row; whether a disabled language may be used
// is policy, and every write path answers it here so the rejection code stays
// consistent between submissions and runs.
func requireLanguageEnabled(language LanguageRecord) error {
	if language.Enabled {
		return nil
	}
	return apperror.Conflict("submission.language_disabled", "language is disabled")
}

func (s *LanguageService) refreshStats(ctx context.Context) {
	if s.stats != nil {
		s.stats.Refresh(ctx)
	}
}
