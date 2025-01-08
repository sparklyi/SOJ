package initialize

import (
	"SOJ/middleware"
	"SOJ/utils/jwt"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func InitMiddleware(j *jwt.JWT, log *zap.Logger) []gin.HandlerFunc {
	return []gin.HandlerFunc{
		//token鉴权
		middleware.NewJWTMiddleware(j).JWTAuth(),
		//跨域
		middleware.CrossDomain(),
	}
}
