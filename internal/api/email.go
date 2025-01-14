package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func EmailRoute(r *gin.RouterGroup, e *handle.EmailHandler) {
	email := r.Group("/email")
	{
		email.POST("/send", e.SendEmailCode)
	}
}
