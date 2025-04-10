package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func SubmissionRoute(r *gin.RouterGroup, s handle.SubmissionHandle, mid []gin.HandlerFunc) {

	submission := r.Group("submission").Use(mid[1])
	{
		submission.POST("run", s.Run)                           //自测运行
		submission.POST("judge", s.Judge)                       //提交运行
		submission.POST("list", s.List)                         //测评列表
		submission.GET("/:sid", s.GetInfoByID)                  //测评详情
		submission.GET("/rank/:pid", s.GetJudgeRankByProblemID) // 获取题目时空排行榜
		//TODO 竞赛结束修改测评为可见

	}

}
