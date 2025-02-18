package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func ProblemRoute(r *gin.RouterGroup, p *handle.ProblemHandle, mid []gin.HandlerFunc) {
	problem := r.Group("problem")
	{
		//problem.GET("/set/:tag")
		problem.GET("/total", mid[1], p.Count)
		problem.GET("/:pid", mid[1], p.Detail)
		problem.POST("/", p.List)
		problem.POST("/create", mid[1], mid[2], p.Create)
		problem.PUT("/update", mid[1], mid[2], p.UpdateInfo)
		problem.DELETE("/:pid", mid[1], mid[3], p.Delete)

		//测试点
		problem.GET("/:pid/case", mid[1], mid[2], p.TestCaseInfo)
		problem.POST("/:pid/create", mid[1], mid[2], p.CreateTestCase)
		problem.PUT("/:pid/update", mid[1], mid[2], p.UpdateTestCase)
		//problem.DELETE("/:pid/delete", mid[1], mid[2])
	}
}
