package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func ProblemRoute(r *gin.RouterGroup, p handle.ProblemHandle, mid []gin.HandlerFunc) {
	problem := r.Group("problem")
	{
		//problem.GET("/set/:tag")
		problem.GET("/total", mid[1], p.Count)               //获取题目总数
		problem.GET("/:pid", mid[1], p.Detail)               //获取题目详情
		problem.POST("/", p.List)                            //获取题目列表
		problem.POST("/create", mid[1], mid[2], p.Create)    //题目创建
		problem.PUT("/update", mid[1], mid[2], p.UpdateInfo) //题目更新
		problem.DELETE("/:pid", mid[1], mid[3], p.Delete)    //题目删除

		//测试点
		problem.GET("/:pid/case", mid[1], mid[2], p.TestCaseInfo)      //获取题目测试点
		problem.POST("/:pid/create", mid[1], mid[2], p.CreateTestCase) //创建测试点
		problem.PUT("/:pid/update", mid[1], mid[2], p.UpdateTestCase)  //更新测试点
		//problem.DELETE("/:pid/delete", mid[1], mid[2])
	}
}
