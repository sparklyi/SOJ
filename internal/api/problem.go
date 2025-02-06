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
		problem.GET("/:pid/submission", mid[1])
		problem.POST("/", mid[1], p.List)
		problem.POST("/create", mid[1], mid[2], p.Create)
		problem.PUT("/update", mid[1], mid[2])
		problem.DELETE("/delete", mid[1], mid[2])
		problem.POST("/search", mid[1])

	}
}
