package initialize

import (
	"SOJ/internal/api"
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func InitRoute(
	email *handle.EmailHandler,
	user *handle.UserHandler,
	mid []gin.HandlerFunc,
) *gin.Engine {
	r := gin.Default()
	g := r.Group("/api/v1")
	api.EmailRoute(g, email, mid)
	api.UserRoute(g, user, mid)
	return r
}
