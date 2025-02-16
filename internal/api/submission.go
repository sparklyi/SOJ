package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func SubmissionRoute(r *gin.RouterGroup, s *handle.SubmissionHandle, mid []gin.HandlerFunc) {

	submission := r.Group("submission")
	{
		submission.POST("run", mid[1], s.Run)
		submission.POST("judge", mid[1], s.Judge)
		submission.POST("list", mid[1], s.List)
		submission.GET("/:sid", mid[1], s.GetInfoByID)
		//TODO 竞赛结束修改测评为可见

	}

}
