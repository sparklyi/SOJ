package initialize

import (
	"SOJ/internal/api"
	"SOJ/internal/handle"
	"github.com/gin-gonic/gin"
)

func InitRoute(
	captcha *handle.CaptchaHandle,
	email *handle.EmailHandle,
	user *handle.UserHandle,

	mid []gin.HandlerFunc,
) *gin.Engine {
	r := gin.Default()
	r.Use(mid[0])
	g := r.Group("/api/v1")
	api.CaptchaRoute(g, captcha, mid)
	api.EmailRoute(g, email, mid)
	api.UserRoute(g, user, mid)
	return r
}
