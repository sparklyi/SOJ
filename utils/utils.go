package utils

import (
	"SOJ/utils/jwt"
	"crypto/sha1"
	"encoding/hex"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"gorm.io/gorm"
	"math/rand"
)

// GenerateRandCode 生成长度为length的随机码
func GenerateRandCode(length int, digit bool) string {
	tab := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if digit {
		tab = "0123456789"
	}
	code := make([]byte, length)
	for i := 0; i < length; i++ {
		code[i] = tab[rand.Intn(len(tab))]
	}
	return string(code)

}

// Paginate 分页处理
func Paginate(page, PageSize int) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if page <= 0 {
			page = 1
		}
		if PageSize <= 0 || PageSize >= 200 {
			PageSize = 20
		}

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

func UnmarshalBSON(data interface{}, target interface{}) error {
	bd, err := bson.Marshal(data)
	if err != nil {
		return err
	}
	return bson.Unmarshal(bd, target)
}
