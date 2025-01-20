package utils

import (
	"SOJ/utils/jwt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"math/rand"
)

// GenerateRandCode 生成长度为length的随机验证码
func GenerateRandCode(length int) string {
	digits := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	code := make([]byte, length)
	for i := 0; i < length; i++ {
		code[i] = digits[rand.Intn(len(digits))]
	}
	return string(code)

}

// Paginate 分页处理
func Paginate(page, PageSize int) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Offset((page - 1) * PageSize).Limit(PageSize)
	}
}

func GetAccessClaims(c *gin.Context) *jwt.AccessClaims {
	t, ok := c.Get("claims")
	if !ok {
		return nil
	}
	claims, _ := t.(*jwt.AccessClaims)

	return claims
}
