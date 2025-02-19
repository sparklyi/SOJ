package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func ContestRoute(r *gin.RouterGroup, c *handle.ContestHandle, mid []gin.HandlerFunc) {
	contest := r.Group("contest").Use(mid[1])
	{
		contest.GET("/:cid", c.GetInfoByID)
		contest.GET("/u/:uid", c.GetListByUserID)
		contest.POST("/", c.GetContestList)
		contest.POST("/create", c.CreateContest)
		contest.PUT("/update", c.UpdateContest)
		contest.DELETE("/:cid", c.DeleteContest)

	}
}
