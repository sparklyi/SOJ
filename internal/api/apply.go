package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func ApplyRoute(r *gin.RouterGroup, a *handle.ApplyHandle, mid []gin.HandlerFunc) {
	apply := r.Group("apply").Use(mid[1])
	{
		apply.GET("/self")          //获取个人报名列表
		apply.POST("/:cid")         //报名
		apply.POST("/list", mid[2]) //管理员获取报名列表
		apply.PUT("/:aid", mid[2])  //管理员修改报名信息
		apply.DELETE("/:aid")       //取消报名

	}
}
