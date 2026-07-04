package submission

import "github.com/gin-gonic/gin"

type Module struct {
	handler *Handler
}

func NewModule(handler *Handler) *Module {
	return &Module{handler: handler}
}

func (m *Module) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/submissions", m.handler.CreateSubmission)
	group.GET("/submissions", m.handler.ListSubmissions)
	group.GET("/submissions/:id", m.handler.GetSubmission)
	group.POST("/runs", m.handler.CreateRun)
	group.GET("/runs/:id", m.handler.GetRun)
	admin := group.Group("/admin")
	admin.GET("/languages", m.handler.ListLanguages)
	admin.POST("/languages/sync", m.handler.SyncLanguages)
	admin.PATCH("/languages/:id", m.handler.UpdateLanguage)
}
