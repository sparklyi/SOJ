// Package stats owns the site-level aggregate (published problems,
// total submissions, enabled languages) rendered on the home page.
//
// 口径与架构约定（与 README 一致）：PostgreSQL 是事实源，Redis 只是缓存。
// 读路径是 cache-aside：先查 Redis，未命中（或 Redis 出错）回源 PostgreSQL；
// 写路径由各领域服务在自身 PostgreSQL 写入提交成功后调用 Refresh——
// 顺序永远是「先更新 PG，再更新 Redis」，缓存里不会出现比事实源更旧的值。
package stats

import (
	"context"
	"log/slog"
)

// Facts is the site aggregate served by GET /api/v1/stats/site.
type Facts struct {
	Problems    int64 `json:"problems"`
	Submissions int64 `json:"submissions"`
	Languages   int64 `json:"languages"`
}

// Store reads the aggregate from PostgreSQL, the source of truth.
// 用原生 pgx 而不是 sqlc：三个标量 COUNT 不值得生成一套模型。
type Store interface {
	CountPublishedProblems(ctx context.Context) (int64, error)
	CountSubmissions(ctx context.Context) (int64, error)
	CountEnabledLanguages(ctx context.Context) (int64, error)
}

// Cache holds the precomputed aggregate in Redis.
type Cache interface {
	// Get returns the cached facts; found is false on a cache miss.
	Get(ctx context.Context) (Facts, bool, error)
	// Set repopulates the cache entry.
	Set(ctx context.Context, facts Facts) error
}

// Service serves the aggregate and refreshes the cache after domain writes.
type Service struct {
	store  Store
	cache  Cache // nil 合法：没有 Redis 时退化为每次直查 PG
	logger *slog.Logger
}

func NewService(store Store, cache Cache, logger *slog.Logger) *Service {
	if store == nil {
		panic("stats store is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, cache: cache, logger: logger}
}

// SiteFacts reads the aggregate with cache-aside semantics.
func (s *Service) SiteFacts(ctx context.Context) (Facts, error) {
	if s.cache != nil {
		facts, found, err := s.cache.Get(ctx)
		if err == nil && found {
			return facts, nil
		}
		if err != nil {
			// 缓存故障不阻断读：回源 PG，首页不能因为 Redis 抖动而 500。
			s.logger.WarnContext(ctx, "stats cache read failed; falling back to postgres", "error", err)
		}
	}
	facts, err := s.readFromStore(ctx)
	if err != nil {
		return Facts{}, err
	}
	s.refreshCache(ctx, facts)
	return facts, nil
}

// Refresh recomputes the aggregate from PostgreSQL and repopulates the cache.
// 领域服务在自己的 PG 写入提交成功后调用；失败只记日志——
// 缓存脏一拍会由下一次读或写路径自愈，不该让一次提交为此报错。
func (s *Service) Refresh(ctx context.Context) {
	facts, err := s.readFromStore(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "stats refresh: postgres read failed", "error", err)
		return
	}
	s.refreshCache(ctx, facts)
}

func (s *Service) readFromStore(ctx context.Context) (Facts, error) {
	problems, err := s.store.CountPublishedProblems(ctx)
	if err != nil {
		return Facts{}, err
	}
	submissions, err := s.store.CountSubmissions(ctx)
	if err != nil {
		return Facts{}, err
	}
	languages, err := s.store.CountEnabledLanguages(ctx)
	if err != nil {
		return Facts{}, err
	}
	return Facts{Problems: problems, Submissions: submissions, Languages: languages}, nil
}

func (s *Service) refreshCache(ctx context.Context, facts Facts) {
	if s.cache == nil {
		return
	}
	if err := s.cache.Set(ctx, facts); err != nil {
		s.logger.WarnContext(ctx, "stats cache write failed", "error", err)
	}
}
