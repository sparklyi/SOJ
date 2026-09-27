package submission

import (
	"SOJ/internal/auth"

	"github.com/gin-gonic/gin"
)

func int64Ptr(value int64) *int64 { return &value }

// actorAuthMiddleware injects an explicit actor for handler tests that need an
// actor with assigned roles. The X-User-Role fallback only fills the legacy
// single-role field, which the permission checks do not read.
func actorAuthMiddleware(actor auth.Actor) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("actor", actor)
		c.Next()
	}
}
