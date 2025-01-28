package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func ProblemRoute(r *gin.RouterGroup, p *handle.ProblemHandle, mid []gin.HandlerFunc) {
	problem := r.Group("problem")
	{
		problem.GET("/list")
		problem.GET("/:pid")
		problem.POST("/create", mid[1], mid[2])
		problem.PUT("/update", mid[1], mid[2])
		problem.DELETE("/delete", mid[1], mid[2])
		problem.POST("/search", mid[1])
	}
}
