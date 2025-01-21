package utils

import (
	"SOJ/utils/jwt"
	"crypto/sha1"
	"encoding/hex"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"math/rand"
)

// GenerateRandCode 生成长度为length的随机码
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

// GetAccessClaims 获取短token的声明
func GetAccessClaims(c *gin.Context) *jwt.AccessClaims {
	t, ok := c.Get("claims")
	if !ok {
		return nil
	}
	claims, _ := t.(*jwt.AccessClaims)

	return claims
}

func CryptoSHA1(str string) string {
	hash := sha1.New()
	hash.Write([]byte(str))
	return hex.EncodeToString(hash.Sum(nil))
}
