package middleware

import (
	"SOJ/utils/jwt"
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"
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
		//不需要鉴权的api
		//url := c.Request.URL.Path
		//strings.Split(url, "/")

		//得到短token
		token := c.GetHeader("SOJ-Access-Token")
		if token == "" {
			response.UnauthorizedErrorWithMsg(c, "未登录")
			c.Abort()
			return
		}
		//解析短token
		claims, err := j.ParseAccess(token)
		if err != nil {
			response.UnauthorizedErrorWithMsg(c, "token无效")
			c.Abort()
			return
		}
		//把声明存入context 后续请求使用
		c.Set("claims", claims)
		c.Next()
	}

}
