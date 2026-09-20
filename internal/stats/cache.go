package stats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// cacheKey 带版本号：口径变了（比如「题目数」从全部改成已发布）就换 key，
// 让旧值自然过期，而不是读到一条口径不一致的缓存。
const cacheKey = "soj:stats:site:v1"

// RedisCache is the Redis-backed Cache implementation.
type RedisCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisCache builds a cache. ttl 是兜底过期时间：正常情况下每次
// 领域写入都会刷新缓存，TTL 只在写入方长期静默时保证最终一致。
func NewRedisCache(client *redis.Client, ttl time.Duration) *RedisCache {
	return &RedisCache{client: client, ttl: ttl}
}

func (c *RedisCache) Get(ctx context.Context) (Facts, bool, error) {
	payload, err := c.client.Get(ctx, cacheKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return Facts{}, false, nil
	}
	if err != nil {
		return Facts{}, false, err
	}
	var facts Facts
	if err := json.Unmarshal(payload, &facts); err != nil {
		return Facts{}, false, fmt.Errorf("stats: decode cached facts: %w", err)
	}
	return facts, true, nil
}

func (c *RedisCache) Set(ctx context.Context, facts Facts) error {
	payload, err := json.Marshal(facts)
	if err != nil {
		return fmt.Errorf("stats: encode facts: %w", err)
	}
	return c.client.Set(ctx, cacheKey, payload, c.ttl).Err()
}

// PostgresStore reads the aggregate with plain scalar queries.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// CountPublishedProblems 数的是**访客可见**的题目：已发布且公开，
// 与题目列表页匿名口径一致（多一道草稿都不算「这个站有多大」）。
func (s *PostgresStore) CountPublishedProblems(ctx context.Context) (int64, error) {
	return queryInt64(ctx, s.pool, `SELECT count(*) FROM problems WHERE status = 'published' AND visibility = 'public'`)
}

func (s *PostgresStore) CountSubmissions(ctx context.Context) (int64, error) {
	return queryInt64(ctx, s.pool, `SELECT count(*) FROM submissions`)
}

func (s *PostgresStore) CountEnabledLanguages(ctx context.Context) (int64, error) {
	return queryInt64(ctx, s.pool, `SELECT count(*) FROM languages WHERE enabled = true`)
}

func queryInt64(ctx context.Context, pool *pgxpool.Pool, sql string) (int64, error) {
	var count int64
	if err := pool.QueryRow(ctx, sql).Scan(&count); err != nil {
		return 0, fmt.Errorf("stats: %w", err)
	}
	return count, nil
}
