package middleware

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func CrossDomain() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "POST,GET,OPTIONS,PUT,DELETE,PATCH")
		c.Header("Access-Control-Allow-Headers", "Content-Type,AccessToken,X-CSRF-Token, Authorization,SOJ-Access-Token,SOJ-Refresh-Token")
		c.Header("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == "OPTIONS" {
			//终端并返回状态码204
			c.AbortWithStatus(http.StatusNoContent)
		}
		c.Next()
	}
}
