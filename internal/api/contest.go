package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func ContestRoute(r *gin.RouterGroup, c handle.ContestHandle, mid []gin.HandlerFunc) {

	contest := r.Group("contest")
	//不需要鉴权的API
	{
		contest.POST("/list", c.GetContestList) //用于未登录用户查看列表
	}
	contest.Use(mid[1])
	{
		contest.GET("/:cid", c.GetInfoByID)       //获取比赛详情
		contest.GET("/u/:uid", c.GetListByUserID) //获取user创建的比赛
		contest.POST("", c.GetContestList)        //获取近期比赛列表
		contest.POST("/create", c.CreateContest)  //创建比赛
		contest.PUT("/update", c.UpdateContest)   //更新比赛信息
		contest.DELETE("/:cid", c.DeleteContest)  //删除比赛
		contest.POST("/rank", c.GetRankingList)   //比赛成绩排行榜
	}
}
