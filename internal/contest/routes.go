package contest

import "github.com/gin-gonic/gin"

type Module struct {
	handler *Handler
}

func NewModule(service *Service) *Module {
	return &Module{handler: NewHandler(service)}
}

func (m *Module) RegisterRoutes(group *gin.RouterGroup) {
	contests := group.Group("/contests")
	contests.POST("", m.handler.createContest)
	contests.GET("", m.handler.listContests)
	contests.GET("/:id", m.handler.getContest)
	contests.PATCH("/:id", m.handler.updateContest)
	contests.DELETE("/:id", m.handler.deleteContest)
	contests.POST("/:id/registrations", m.handler.register)
	contests.GET("/:id/scoreboard", m.handler.scoreboard)
}
