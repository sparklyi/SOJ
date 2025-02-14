package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func LanguageRoute(r *gin.RouterGroup, l *handle.LanguageHandle, mid []gin.HandlerFunc) {
	lang := r.Group("language")
	{
		lang.POST("", mid[1], l.List)
		lang.POST("/sync", mid[1], mid[3], l.SyncLanguage)
		lang.PUT("/update", mid[1], mid[3], l.Update)
		lang.GET("/:lid", mid[1], mid[2], l.GetInfo)
	}
}
