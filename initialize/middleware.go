package initialize

import (
	"SOJ/middleware"
	"SOJ/utils/jwt"
	"github.com/gin-gonic/gin"
)

func InitMiddleware(j *jwt.JWT) []gin.HandlerFunc {
	return []gin.HandlerFunc{
		//跨域      mid[0]
		middleware.CrossDomain(),
		//token鉴权 mid[1]
		middleware.NewJWTMiddleware(j).JWTAuth(),
		//admin权限 mid[2]
		middleware.AdminAuth(),
		//root权限  mid[3]
		middleware.RootAuth(),
		//限流      mid[4]
		middleware.RateLimit(),
	}
}
