package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"SOJ/internal/auth"
	"SOJ/internal/authz"
	"SOJ/internal/user"

	"github.com/gin-gonic/gin"
)

type actorResolverStub struct {
	state user.ActorState
	err   error
}

func (s actorResolverStub) ResolveActor(context.Context, int64) (user.ActorState, error) {
	return s.state, s.err
}

// actorMiddleware resolves the account on every request so a disabled account
// loses its session immediately instead of when its access token expires.
func TestActorMiddlewareDropsDisabledSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwtManager := auth.NewJWTManager("secret", time.Minute)
	token, err := jwtManager.IssueAccessToken(auth.Actor{UserID: 42})
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v", err)
	}

	tests := map[string]struct {
		resolver  actorResolverStub
		wantAuth  bool
		wantPerms []authz.Permission
	}{
		"active account keeps its session": {
			resolver: actorResolverStub{state: user.ActorState{
				Status:      user.StatusActive,
				Roles:       []auth.Role{auth.RoleAuthor},
				Permissions: []authz.Permission{authz.PermissionProblemCreate},
			}},
			wantAuth:  true,
			wantPerms: []authz.Permission{authz.PermissionProblemCreate},
		},
		"disabled account becomes anonymous": {
			resolver: actorResolverStub{state: user.ActorState{Status: user.StatusDisabled, Roles: []auth.Role{auth.RoleAuthor}}},
		},
		"unknown account becomes anonymous": {
			resolver: actorResolverStub{err: user.ErrNotFound},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			router := gin.New()
			router.Use(actorMiddleware(jwtManager, tt.resolver))
			router.GET("/whoami", func(c *gin.Context) {
				actor, _ := c.Get(user.ActorContextKey)
				resolved, _ := actor.(auth.Actor)
				if resolved.Authenticated() != tt.wantAuth {
					t.Fatalf("authenticated = %v, want %v (actor %+v)", resolved.Authenticated(), tt.wantAuth, resolved)
				}
				if len(resolved.Permissions) != len(tt.wantPerms) {
					t.Fatalf("permissions = %v, want %v", resolved.Permissions, tt.wantPerms)
				}
				for i := range tt.wantPerms {
					if resolved.Permissions[i] != tt.wantPerms[i] {
						t.Fatalf("permissions = %v, want %v", resolved.Permissions, tt.wantPerms)
					}
				}
				c.Status(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodGet, "/whoami", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
			}
		})
	}
}
