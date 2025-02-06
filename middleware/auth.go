package middleware

import (
	"SOJ/internal/constant"
	"SOJ/utils"
	"SOJ/utils/response"
	"github.com/gin-gonic/gin"
)

func AdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := utils.GetAccessClaims(c)
		if claims == nil || claims.Auth < constant.AdminLevel {
			response.UnauthorizedErrorWithMsg(c, constant.UnauthorizedError)
			c.Abort()
			return
		}
		c.Next()

	}
}

func RootAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := utils.GetAccessClaims(c)
		if claims == nil || claims.Auth < constant.RootLevel {
			response.UnauthorizedErrorWithMsg(c, constant.UnauthorizedError)
			c.Abort()
			return
		}
		c.Next()
	}
}
