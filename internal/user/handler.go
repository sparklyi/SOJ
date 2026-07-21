package user

import (
	"context"
	"net/http"
	"strconv"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/httpapi"

	"github.com/gin-gonic/gin"
)

const ActorContextKey = "actor"

type HandlerService interface {
	Register(context.Context, auth.Actor, RegisterInput) (AuthSession, error)
	Login(context.Context, auth.Actor, LoginInput) (AuthSession, error)
	Refresh(context.Context, auth.Actor, RefreshInput) (AuthSession, error)
	Logout(context.Context, auth.Actor, LogoutInput) error
	Me(context.Context, auth.Actor) (User, error)
	ListUsers(context.Context, auth.Actor, ListUsersInput) (UserList, error)
	ListUsersByCursor(context.Context, auth.Actor, ListUsersInput) (UserCursorPage, error)
	UpdateUser(context.Context, auth.Actor, int64, UpdateUserInput) (User, error)
}

type Handler struct {
	service HandlerService
}

func NewHandler(service HandlerService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Register(c *gin.Context) {
	var input RegisterInput
	if !bindJSON(c, &input) {
		return
	}
	session, err := h.service.Register(c.Request.Context(), actorFromGin(c), input)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.Created(c, session)
}

func (h *Handler) Login(c *gin.Context) {
	var input LoginInput
	if !bindJSON(c, &input) {
		return
	}
	session, err := h.service.Login(c.Request.Context(), actorFromGin(c), input)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, session)
}

func (h *Handler) Refresh(c *gin.Context) {
	var input RefreshInput
	if !bindJSON(c, &input) {
		return
	}
	session, err := h.service.Refresh(c.Request.Context(), actorFromGin(c), input)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, session)
}

func (h *Handler) Logout(c *gin.Context) {
	var input LogoutInput
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if !bindJSON(c, &input) {
			return
		}
	}
	if err := h.service.Logout(c.Request.Context(), actorFromGin(c), input); err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.NoContent(c)
}

func (h *Handler) Me(c *gin.Context) {
	user, err := h.service.Me(c.Request.Context(), actorFromGin(c))
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, user)
}

func (h *Handler) ListUsers(c *gin.Context) {
	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 32)
	pageSize, _ := strconv.ParseInt(c.DefaultQuery("page_size", "20"), 10, 32)
	users, err := h.service.ListUsers(c.Request.Context(), actorFromGin(c), ListUsersInput{
		Role:     c.Query("role"),
		Status:   c.Query("status"),
		Keyword:  c.Query("keyword"),
		Page:     int32(page),
		PageSize: int32(pageSize),
	})
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, users)
}

func (h *Handler) ListUsersByCursor(c *gin.Context) {
	pageSize, err := strconv.ParseInt(c.DefaultQuery("page_size", "20"), 10, 32)
	if err != nil || pageSize <= 0 || pageSize > 100 {
		httpapi.Error(c, apperror.BadRequest("invalid_page_size", "page_size must be between 1 and 100"))
		return
	}
	input := ListUsersInput{
		Role:     c.Query("role"),
		Status:   c.Query("status"),
		Keyword:  c.Query("keyword"),
		PageSize: int32(pageSize),
	}
	if raw, ok := c.GetQuery("cursor"); ok {
		var cursor UserCursor
		if err := httpapi.DecodeCursor(raw, &cursor); err != nil {
			httpapi.Error(c, apperror.BadRequest("invalid_cursor", "cursor is invalid"))
			return
		}
		input.Cursor = &cursor
	}
	page, err := h.service.ListUsersByCursor(c.Request.Context(), actorFromGin(c), input)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	data := gin.H{"items": page.Items}
	if page.NextCursor != nil {
		token, err := httpapi.EncodeCursor(page.NextCursor)
		if err != nil {
			httpapi.Error(c, apperror.Internal())
			return
		}
		data["next_cursor"] = token
	}
	httpapi.OK(c, data)
}

func (h *Handler) UpdateUser(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		httpapi.Error(c, apperror.BadRequest("user.invalid_id", "invalid user id"))
		return
	}
	var input UpdateUserInput
	if !bindJSON(c, &input) {
		return
	}
	user, err := h.service.UpdateUser(c.Request.Context(), actorFromGin(c), id, input)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, user)
}

func bindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		httpapi.Error(c, apperror.BadRequest("bad_request", http.StatusText(http.StatusBadRequest)))
		return false
	}
	return true
}

func actorFromGin(c *gin.Context) auth.Actor {
	if value, ok := c.Get(ActorContextKey); ok {
		if actor, ok := value.(auth.Actor); ok {
			return actor
		}
	}
	if value, ok := c.Get("auth.Actor"); ok {
		if actor, ok := value.(auth.Actor); ok {
			return actor
		}
	}
	return auth.Anonymous(c.GetString(httpapi.ContextRequestID))
}
