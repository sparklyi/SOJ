package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func UserRoute(r *gin.RouterGroup, u handle.UserHandle, mid []gin.HandlerFunc) {
	user := r.Group("/user")
	{
		user.GET("/logout", u.Logout)                                  //退出
		user.GET("/:id", mid[1], u.GetUserInfo)                        //获取用户信息
		user.POST("/register", u.Register)                             //注册
		user.POST("/login_password", u.LoginByPassword)                //密码登录
		user.POST("/login_email", u.LoginByEmail)                      //邮箱登录
		user.POST("/refresh_token", u.RefreshToken)                    //令牌刷新
		user.POST("/avatar", mid[1], u.UploadAvatar)                   //头像上传
		user.POST("/list", mid[1], mid[3], u.GetUserList)              //用户列表
		user.PUT("/update_password", mid[1], mid[3], u.UpdatePassword) //密码更新
		user.PUT("/update", mid[1], u.UpdateUserInfo)                  //用户信息更新
		user.PUT("/:email", mid[1], mid[3], u.ResetPassword)           //重置密码
		user.DELETE("/:id", mid[1], mid[3], u.Delete)                  //用户删除
	}
}
