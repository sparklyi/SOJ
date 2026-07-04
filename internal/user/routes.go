package user

import "github.com/gin-gonic/gin"

type Module struct {
	handler *Handler
}

func NewModule(service HandlerService) *Module {
	return &Module{handler: NewHandler(service)}
}

func (m *Module) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/auth/register", m.handler.Register)
	group.POST("/auth/login", m.handler.Login)
	group.POST("/auth/refresh", m.handler.Refresh)
	group.POST("/auth/logout", m.handler.Logout)
	group.GET("/me", m.handler.Me)
	group.GET("/admin/users", m.handler.ListUsers)
	group.PATCH("/admin/users/:id", m.handler.UpdateUser)
}
