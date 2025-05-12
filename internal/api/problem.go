package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func ProblemRoute(r *gin.RouterGroup, p handle.ProblemHandle, mid []gin.HandlerFunc) {
	problem := r.Group("problem")

	//不需要鉴权的API
	{
		problem.GET("/:pid/judge_count", p.JudgeStatusCount) //获取题目测评总数
		problem.POST("", p.List)                             //获取题目列表
	}
	//需要鉴权
	problem.Use(mid[1])
	{
		//problem.GET("/set/:tag")
		problem.POST("/user", p.GetUserProblemList)  //获取用户题目
		problem.GET("/:pid", p.Detail)               //获取题目详情
		problem.POST("/list", p.List)                //带token的题目列表查询
		problem.POST("/create", mid[2], p.Create)    //题目创建
		problem.PUT("/update", mid[2], p.UpdateInfo) //题目更新
		problem.DELETE("/:pid", mid[2], p.Delete)    //题目删除
		//测试点
		problem.GET("/:pid/case", mid[2], p.TestCaseInfo)      //获取题目测试点
		problem.POST("/:pid/create", mid[2], p.CreateTestCase) //创建测试点
		problem.PUT("/:pid/update", mid[2], p.UpdateTestCase)  //更新测试点
		//problem.DELETE("/:pid/delete", mid[1], mid[2])
	}
}
