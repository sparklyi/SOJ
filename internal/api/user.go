package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func UserRoute(r *gin.RouterGroup, u *handle.UserHandler, mid []gin.HandlerFunc) {
	user := r.Group("/user")
	{
		user.POST("/register", u.Register)
	}
}
