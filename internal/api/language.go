package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func LanguageRoute(r *gin.RouterGroup, l *handle.LanguageHandle, mid []gin.HandlerFunc) {
	lang := r.Group("language").Use(mid[1])
	{
		lang.POST("", l.List)
		lang.POST("/sync", mid[3], l.SyncLanguage)
		lang.PUT("/update", mid[3], l.Update)
		lang.GET("/:lid", mid[2], l.GetInfo)
	}
}
