package problem

import (
	"strconv"

	"SOJ/internal/auth"
	"SOJ/internal/httpapi"

	"github.com/gin-gonic/gin"
)

// testMiddlewareSet mirrors the production auth middleware for handler tests:
// it turns the legacy X-User-ID/X-User-Role headers into an actor carrying the
// seeded permissions. Production reads the JWT and the database; this keeps the
// existing header-driven handler tests meaningful now that authorization reads
// Actor.Permissions.
func testMiddlewareSet() httpapi.MiddlewareSet {
	middleware := httpapi.DefaultMiddlewareSet()
	middleware.Auth = func(c *gin.Context) {
		var actor auth.Actor
		if userID, err := strconv.ParseInt(c.GetHeader("X-User-ID"), 10, 64); err == nil {
			actor.UserID = userID
		}
		if role, err := auth.ParseRole(c.GetHeader("X-User-Role")); err == nil {
			actor.Roles = []auth.Role{role}
			actor.Permissions = seededPermissions(role)
		}
		actor.RequestID = c.GetString(httpapi.ContextRequestID)
		c.Set("actor", actor)
		c.Next()
	}
	return middleware
}
