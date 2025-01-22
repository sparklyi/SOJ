package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func EmailRoute(r *gin.RouterGroup, e *handle.EmailHandler, mid []gin.HandlerFunc) {
	email := r.Group("/email")
	{
		email.POST("/register", e.SendRegisterCode)
		email.POST("/reset", mid[1], e.SendResetPwdCode)
	}
}
