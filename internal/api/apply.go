package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func ApplyRoute(r *gin.RouterGroup, a *handle.ApplyHandle, mid []gin.HandlerFunc) {
	apply := r.Group("apply").Use(mid[1])
	{
		apply.GET("/self")
	}
}
