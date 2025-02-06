package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func UserRoute(r *gin.RouterGroup, u *handle.UserHandle, mid []gin.HandlerFunc) {
	user := r.Group("/user")
	{
		user.GET("/logout", u.Logout)
		user.GET("/:id", u.GetUserInfo)
		user.POST("/register", u.Register)
		user.POST("/login_password", u.LoginByPassword)
		user.POST("/login_email", u.LoginByEmail)
		user.POST("/refresh_token", u.RefreshToken)
		user.POST("/avatar", mid[1], u.UploadAvatar)
		user.POST("/list", mid[1], mid[3], u.GetUserList)
		user.PUT("/update_password", mid[1], mid[3], u.UpdatePassword)
		user.PUT("/update", mid[1], u.UpdateUserInfo)
		user.PUT("/:email", mid[1], mid[3], u.ResetPassword)
	}
}
