package initialize

import (
	"SOJ/middleware"
	"SOJ/utils/jwt"
	"github.com/gin-gonic/gin"
)

func InitMiddleware(j *jwt.JWT) []gin.HandlerFunc {
	return []gin.HandlerFunc{
		//跨域       mid[0]
		middleware.CrossDomain(),
		//token鉴权  mid[1]
		middleware.NewJWTMiddleware(j).JWTAuth(),
		//admin权限
		middleware.AdminAuth(),
	}
}
