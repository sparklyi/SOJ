package middleware

import (
	"SOJ/utils/jwt"
	"github.com/gin-gonic/gin"
	"net/http"
)

type JWTMiddleware struct {
	*jwt.JWT
}

// NewJWTMiddleware 依赖注入
func NewJWTMiddleware(j *jwt.JWT) *JWTMiddleware {
	return &JWTMiddleware{j}
}

// JWTAuth jwt验证
func (j *JWTMiddleware) JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		//得到短token
		token := c.Request.Header.Get("SOJ-Access-Token")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"msg": "未登录",
			})
			c.Abort()
			return
		}
		//解析短token
		claims, err := j.ParseAccess(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"msg": err.Error(),
			})
			c.Abort()
			return
		}
		//把声明存入context 后续请求使用
		c.Set("claims", claims)
		c.Set("id", claims.ID)
		c.Next()
	}

}
