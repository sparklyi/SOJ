package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func ContestRoute(r *gin.RouterGroup, c *handle.ContestHandle, mid []gin.HandlerFunc) {
	contest := r.Group("contest")
	{
		contest.POST("/")
		contest.POST("/create", mid[1], c.CreateContest)
		contest.GET("/:cid", mid[1])
		contest.PUT("/update", mid[1], c.UpdateContest)

	}
}
