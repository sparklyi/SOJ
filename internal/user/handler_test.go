package user

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/httpapi"

	"github.com/gin-gonic/gin"
)

type fakeService struct {
	register   func(context.Context, auth.Actor, RegisterInput) (AuthSession, error)
	login      func(context.Context, auth.Actor, LoginInput) (AuthSession, error)
	refresh    func(context.Context, auth.Actor, RefreshInput) (AuthSession, error)
	logout     func(context.Context, auth.Actor, LogoutInput) error
	me         func(context.Context, auth.Actor) (User, error)
	listUsers  func(context.Context, auth.Actor, ListUsersInput) (UserList, error)
	updateUser func(context.Context, auth.Actor, int64, UpdateUserInput) (User, error)
}

func (f fakeService) Register(ctx context.Context, actor auth.Actor, input RegisterInput) (AuthSession, error) {
	return f.register(ctx, actor, input)
}

func (f fakeService) Login(ctx context.Context, actor auth.Actor, input LoginInput) (AuthSession, error) {
	return f.login(ctx, actor, input)
}

func (f fakeService) Refresh(ctx context.Context, actor auth.Actor, input RefreshInput) (AuthSession, error) {
	if f.refresh == nil {
		return AuthSession{}, errors.New("not implemented")
	}
	return f.refresh(ctx, actor, input)
}

func (f fakeService) Logout(ctx context.Context, actor auth.Actor, input LogoutInput) error {
	if f.logout == nil {
		return errors.New("not implemented")
	}
	return f.logout(ctx, actor, input)
}

func (f fakeService) Me(ctx context.Context, actor auth.Actor) (User, error) {
	return f.me(ctx, actor)
}

func (f fakeService) ListUsers(ctx context.Context, actor auth.Actor, input ListUsersInput) (UserList, error) {
	if f.listUsers == nil {
		return UserList{}, errors.New("not implemented")
	}
	return f.listUsers(ctx, actor, input)
}

func (f fakeService) UpdateUser(ctx context.Context, actor auth.Actor, id int64, input UpdateUserInput) (User, error) {
	if f.updateUser == nil {
		return User{}, errors.New("not implemented")
	}
	return f.updateUser(ctx, actor, id, input)
}

func TestHandlerRegisterRejectsBadJSON(t *testing.T) {
	router := testRouter(fakeService{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(`{`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlerRegisterMapsConflict(t *testing.T) {
	router := testRouter(fakeService{
		register: func(context.Context, auth.Actor, RegisterInput) (AuthSession, error) {
			return AuthSession{}, apperror.Conflict("user.email_conflict", "email already exists")
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/register", jsonBody(RegisterInput{
		Email:    "taken@example.com",
		Username: "taken",
		Password: "password123",
	}))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestHandlerLoginMapsUnauthorized(t *testing.T) {
	router := testRouter(fakeService{
		login: func(context.Context, auth.Actor, LoginInput) (AuthSession, error) {
			return AuthSession{}, apperror.Unauthorized("auth.invalid_credentials", "invalid credentials")
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", jsonBody(LoginInput{
		Email:    "user@example.com",
		Password: "bad-password",
	}))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandlerMeSuccess(t *testing.T) {
	router := testRouter(fakeService{
		me: func(_ context.Context, actor auth.Actor) (User, error) {
			if actor.UserID != 42 {
				t.Fatalf("actor.UserID = %d, want 42", actor.UserID)
			}
			return User{ID: 42, Email: "user@example.com", Username: "user", Role: auth.RoleUser, Status: StatusActive}, nil
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body httpapi.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response json: %v", err)
	}
	if body.Error != nil {
		t.Fatalf("error = %+v, want nil", body.Error)
	}
}

func TestHandlerRefreshSuccess(t *testing.T) {
	router := testRouter(fakeService{
		refresh: func(context.Context, auth.Actor, RefreshInput) (AuthSession, error) {
			return AuthSession{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 60}, nil
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", jsonBody(RefreshInput{RefreshToken: "old-refresh"}))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandlerLogoutSuccessNoContent(t *testing.T) {
	router := testRouter(fakeService{
		logout: func(context.Context, auth.Actor, LogoutInput) error {
			return nil
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
}

func TestHandlerListUsersUsesPagePagination(t *testing.T) {
	var gotInput ListUsersInput
	router := testRouter(fakeService{
		listUsers: func(_ context.Context, _ auth.Actor, input ListUsersInput) (UserList, error) {
			gotInput = input
			return UserList{Items: []User{}, Total: 0, Page: input.Page, PageSize: input.PageSize}, nil
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users?page=2&page_size=30", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotInput.Page != 2 || gotInput.PageSize != 30 {
		t.Fatalf("pagination = page %d page_size %d, want 2/30", gotInput.Page, gotInput.PageSize)
	}
}

func TestHandlerUpdateUserSuccess(t *testing.T) {
	router := testRouter(fakeService{
		updateUser: func(_ context.Context, _ auth.Actor, id int64, input UpdateUserInput) (User, error) {
			if id != 7 {
				t.Fatalf("id = %d, want 7", id)
			}
			if input.Username == nil || *input.Username != "new-name" {
				t.Fatalf("username = %#v, want new-name", input.Username)
			}
			return User{ID: id, Email: "user@example.com", Username: *input.Username, Role: auth.RoleUser, Status: StatusActive}, nil
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/admin/users/7", bytes.NewBufferString(`{"username":"new-name"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func testRouter(service HandlerService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(httpapi.RequestID())
	router.Use(func(c *gin.Context) {
		c.Set(ActorContextKey, auth.Actor{UserID: 42, Role: auth.RoleUser, DeviceID: "device-1"})
		c.Next()
	})
	NewModule(service).RegisterRoutes(&router.RouterGroup)
	return router
}

func jsonBody(v any) *bytes.Reader {
	body, _ := json.Marshal(v)
	return bytes.NewReader(body)
}
