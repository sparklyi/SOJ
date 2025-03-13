package middleware

import (
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
	"sync"
	"time"
)

type Limiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	limitMap  = make(map[string]*Limiter)
	rateLimit = rate.Every(1 * time.Second)
	//突发请求
	burstLimit = 20
	mu         sync.Mutex
)

func GetLimiter(ip string) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()
	limiter, ok := limitMap[ip]
	//不存在则新建一个
	if !ok {
		limiter = &Limiter{
			limiter:  rate.NewLimiter(rateLimit, burstLimit),
			lastSeen: time.Now(),
		}
		limitMap[ip] = limiter
	} else {
		limiter.lastSeen = time.Now()
	}
	return limiter.limiter

}
func DelLimiter() {
	mu.Lock()
	defer mu.Unlock()
	for i, limit := range limitMap {
		if time.Since(limit.lastSeen) > 16*time.Hour {
			delete(limitMap, i)
		}
	}
}
func RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := GetLimiter(ip)
		if !limiter.Allow() {
			response.TooManyErrorAndMsg(c, "请求频繁")
			c.Abort()
			return
		}
		c.Next()
	}
}
