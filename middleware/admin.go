package middleware

import (
	"SOJ/utils"
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"
)

func AdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := utils.GetAccessClaims(c)
		if claims == nil || claims.Auth != 3 {
			response.UnauthorizedErrorWithMsg(c, "无对应权限")
			c.Abort()
			return
		}
		c.Next()

	}
}
