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
	problem *handle.ProblemHandle,
	language *handle.LanguageHandle,
	submission *handle.SubmissionHandle,
	contest *handle.ContestHandle,
	mid []gin.HandlerFunc,
) *gin.Engine {
	r := gin.Default()
	r.Use(mid[0])
	g := r.Group("/api/v1")
	//各级路由注册
	api.CaptchaRoute(g, captcha, mid)
	api.EmailRoute(g, email, mid)
	api.UserRoute(g, user, mid)
	api.ProblemRoute(g, problem, mid)
	api.LanguageRoute(g, language, mid)
	api.SubmissionRoute(g, submission, mid)
	api.ContestRoute(g, contest, mid)
	return r
}
