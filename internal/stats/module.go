package stats

import (
	"github.com/gin-gonic/gin"

	"SOJ/internal/httpapi"
)

// Module registers the public site-stats route.
type Module struct {
	service *Service
}

func NewModule(service *Service) *Module {
	return &Module{service: service}
}

func (m *Module) RegisterRoutes(group *gin.RouterGroup) {
	group.GET("/stats/site", m.siteFacts)
}

func (m *Module) siteFacts(c *gin.Context) {
	facts, err := m.service.SiteFacts(c.Request.Context())
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, facts)
}
