package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func ApplyRoute(r *gin.RouterGroup, a handle.ApplyHandle, mid []gin.HandlerFunc) {
	apply := r.Group("apply").Use(mid[1])
	{
		apply.POST("/add", a.CreateApply)            //报名
		apply.GET("/self", a.GetListByUserID)        //获取个人报名列表
		apply.POST("/list", mid[2], a.GetList)       //管理员获取报名列表
		apply.PUT("/update", a.UpdateApply)          //修改报名信息
		apply.DELETE("/:aid", mid[2], a.DeleteApply) //取消报名
		apply.POST("/check", mid[1], a.CheckApply)   //检查报名
		//apply.GET("/:aid", mid[2], a.GetInfoByID)    //报名详情

	}
}
