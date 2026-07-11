package problem

import "github.com/gin-gonic/gin"

type Module struct {
	handler *Handler
}

func NewModule(service *Service) *Module {
	return &Module{handler: NewHandler(service)}
}

func (m *Module) RegisterRoutes(group *gin.RouterGroup) {
	problems := group.Group("/problems")
	problems.POST("", m.handler.createProblem)
	problems.GET("", m.handler.listProblems)
	problems.GET("/:id", m.handler.getProblem)
	problems.GET("/:id/authoring", m.handler.getProblemAuthoringState)
	problems.PATCH("/:id", m.handler.updateProblem)
	problems.DELETE("/:id", m.handler.archiveProblem)
	problems.POST("/:id/statement", m.handler.createStatement)
	problems.GET("/:id/statement", m.handler.currentStatement)
	problems.POST("/:id/testcase-sets", m.handler.uploadTestcases)
	problems.POST("/:id/checks", m.handler.runProblemCheck)
	problems.GET("/:id/checks/:check_id", m.handler.getProblemCheck)
	problems.GET("/:id/stats", m.handler.stats)
}
