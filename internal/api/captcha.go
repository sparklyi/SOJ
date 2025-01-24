package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func CaptchaRoute(r *gin.RouterGroup, c *handle.CaptchaHandle, mid []gin.HandlerFunc) {
	email := r.Group("/captcha")
	{
		email.POST("/create", c.GenerateCaptcha)
	}
}
