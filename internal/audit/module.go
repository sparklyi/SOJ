package audit

import "github.com/gin-gonic/gin"

type Module struct {
	handler *Handler
}

func NewModule(store Store) *Module {
	return &Module{handler: NewHandler(NewService(store))}
}

func (m *Module) RegisterRoutes(group *gin.RouterGroup) {
	group.GET("/admin/audit-events", m.handler.listEvents)
}
