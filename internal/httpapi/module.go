package httpapi

import "github.com/gin-gonic/gin"

type Module interface {
	RegisterRoutes(group *gin.RouterGroup)
}
