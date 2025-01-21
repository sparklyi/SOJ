package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func UserRoute(r *gin.RouterGroup, u *handle.UserHandler, mid []gin.HandlerFunc) {
	user := r.Group("/user")
	{
		user.POST("/register", u.Register)
		user.POST("/login_email", u.LoginByEmail)
		user.POST("/refresh_token", u.RefreshToken)
		user.GET("/logout", u.Logout)
		user.GET("/:id", u.GetUserInfo)
		user.POST("/avatar", mid[1], u.UploadAvatar)
	}
}
