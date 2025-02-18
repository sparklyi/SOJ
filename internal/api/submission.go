package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func SubmissionRoute(r *gin.RouterGroup, s *handle.SubmissionHandle, mid []gin.HandlerFunc) {

	submission := r.Group("submission").Use(mid[1])
	{
		submission.POST("run", s.Run)
		submission.POST("judge", s.Judge)
		submission.POST("list", s.List)
		submission.GET("/:sid", s.GetInfoByID)
		//TODO 竞赛结束修改测评为可见

	}

}
