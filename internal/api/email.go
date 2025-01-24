package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func EmailRoute(r *gin.RouterGroup, e *handle.EmailHandle, mid []gin.HandlerFunc) {
	email := r.Group("/email")
	{
		email.POST("/verify_code", e.SendVerifyCode)
	}
}
