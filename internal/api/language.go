package api

import (
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func LanguageRoute(r *gin.RouterGroup, l handle.LanguageHandle, mid []gin.HandlerFunc) {
	lang := r.Group("language").Use(mid[1])
	{
		lang.POST("", l.List)                      //获取测评语言列表
		lang.POST("/sync", mid[3], l.SyncLanguage) //同步postgres中的语言到MySQL
		lang.PUT("/update", mid[3], l.Update)      //测评语言更新(启用或停用)
		lang.GET("/:lid", mid[2], l.GetInfo)       //获取测评语言详情
	}
}
