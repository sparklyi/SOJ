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
	// Register static routes before the submission ID parameter route.
	group.GET("/submissions/mine", m.handler.ListOwnSubmissionsByCursor)
	group.GET("/submissions/:id", m.handler.GetSubmission)
	group.POST("/runs", m.handler.CreateRun)
	group.GET("/runs/:id", m.handler.GetRun)
	group.GET("/languages", m.handler.ListPublicLanguages)
	if m.handler.rejudge != nil {
		group.POST("/rejudge-batches", m.handler.CreateRejudgeBatch)
		group.GET("/rejudge-batches", m.handler.ListRejudgeBatches)
		group.GET("/rejudge-batches/:id", m.handler.GetRejudgeBatch)
		group.POST("/rejudge-batches/:id/cancel", m.handler.CancelRejudgeBatch)
	}
	admin := group.Group("/admin")
	admin.GET("/languages", m.handler.ListLanguages)
	admin.POST("/languages/sync", m.handler.SyncLanguages)
	admin.PATCH("/languages/:id", m.handler.UpdateLanguage)
}
