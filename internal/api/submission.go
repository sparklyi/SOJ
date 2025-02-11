package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func SubmissionRoute(r *gin.RouterGroup, s *handle.SubmissionHandle, mid []gin.HandlerFunc) {

	submission := r.Group("submission")
	{
		submission.POST("run", mid[1], s.Run)
		submission.POST("judge", mid[1])
	}

}
