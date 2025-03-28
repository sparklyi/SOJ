package captcha

import (
	"context"
	"errors"
	"github.com/mojocn/base64Captcha"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"time"
)

const Expire = 1 * time.Minute

type Captcha struct {
	*base64Captcha.Captcha
}

type RedisStore struct {
	rs  *redis.Client
	log *zap.Logger
}

func New(s *RedisStore) *Captcha {
	return &Captcha{
		base64Captcha.NewCaptcha(base64Captcha.NewDriverDigit(80, 240, 6, 0.7, 80), s),
	}

}

func NewRedisStore(r *redis.Client, log *zap.Logger) *RedisStore {
	return &RedisStore{
		rs:  r,
		log: log,
	}
}

func (r *RedisStore) Set(id string, value string) error {
	return r.rs.Set(context.Background(), id, value, Expire).Err()
}

func (r *RedisStore) Get(id string, clear bool) string {
	code, err := r.rs.Get(context.Background(), id).Result()

	if err != nil && !errors.Is(err, redis.Nil) {
		r.log.Error("redis异常", zap.Error(err))
		return ""
	}
	if clear && code != "" {
		r.rs.Del(context.Background(), id)
	}

	return code
}

func (r *RedisStore) Verify(id, answer string, clear bool) bool {
	if id == "" || answer == "" {
		return false
	}
	v := r.Get(id, clear)
	return v != "" && v == answer
}
