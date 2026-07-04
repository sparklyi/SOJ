package contest

import (
	"strconv"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/httpapi"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) createContest(c *gin.Context) {
	var req ContestInput
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.Error(c, apperror.BadRequest("request.invalid", "invalid request body"))
		return
	}
	contest, err := h.service.CreateContest(c.Request.Context(), actorFromContext(c), req)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.Created(c, contest)
}

func (h *Handler) listContests(c *gin.Context) {
	page, ok := int32Query(c, "page", 1)
	if !ok || page <= 0 {
		httpapi.Error(c, apperror.BadRequest("request.invalid", "page must be a positive integer"))
		return
	}
	pageSize, ok := int32Query(c, "page_size", 20)
	if !ok || pageSize <= 0 || pageSize > 100 {
		httpapi.Error(c, apperror.BadRequest("request.invalid", "page_size must be between 1 and 100"))
		return
	}
	list, err := h.service.ListContests(c.Request.Context(), actorFromContext(c), ListContestFilter{
		Status:     c.Query("status"),
		Visibility: c.Query("visibility"),
		Keyword:    c.Query("keyword"),
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, list)
}

func (h *Handler) getContest(c *gin.Context) {
	id, ok := contestIDParam(c)
	if !ok {
		return
	}
	contest, err := h.service.GetContest(c.Request.Context(), actorFromContext(c), id)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, contest)
}

func (h *Handler) updateContest(c *gin.Context) {
	id, ok := contestIDParam(c)
	if !ok {
		return
	}
	var req ContestUpdateInput
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.Error(c, apperror.BadRequest("request.invalid", "invalid request body"))
		return
	}
	contest, err := h.service.UpdateContest(c.Request.Context(), actorFromContext(c), id, req)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, contest)
}

func (h *Handler) deleteContest(c *gin.Context) {
	id, ok := contestIDParam(c)
	if !ok {
		return
	}
	if _, err := h.service.DeleteContest(c.Request.Context(), actorFromContext(c), id); err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.NoContent(c)
}

func (h *Handler) register(c *gin.Context) {
	id, ok := contestIDParam(c)
	if !ok {
		return
	}
	var req RegistrationInput
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.Error(c, apperror.BadRequest("request.invalid", "invalid request body"))
		return
	}
	registration, err := h.service.Register(c.Request.Context(), actorFromContext(c), id, req)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.Created(c, registration)
}

func (h *Handler) scoreboard(c *gin.Context) {
	id, ok := contestIDParam(c)
	if !ok {
		return
	}
	board, err := h.service.Scoreboard(c.Request.Context(), actorFromContext(c), id, ScoreboardView(c.Query("view")))
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, board)
}

func contestIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		httpapi.Error(c, apperror.BadRequest("contest.id_invalid", "contest id is invalid"))
		return 0, false
	}
	return id, true
}

func int32Query(c *gin.Context, key string, fallback int32) (int32, bool) {
	value := c.Query(key)
	if value == "" {
		return fallback, true
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(parsed), true
}

func actorFromContext(c *gin.Context) auth.Actor {
	for _, key := range []string{"actor", "auth.actor"} {
		if value, ok := c.Get(key); ok {
			if actor, ok := value.(auth.Actor); ok {
				return actor
			}
			if actor, ok := value.(*auth.Actor); ok && actor != nil {
				return *actor
			}
		}
	}
	var actor auth.Actor
	if userID, err := strconv.ParseInt(c.GetHeader("X-User-ID"), 10, 64); err == nil {
		actor.UserID = userID
	}
	if role, err := auth.ParseRole(c.GetHeader("X-User-Role")); err == nil {
		actor.Role = role
	}
	if requestID, ok := c.Get(httpapi.ContextRequestID); ok {
		if value, ok := requestID.(string); ok {
			actor.RequestID = value
		}
	}
	if actor.RequestID == "" {
		actor.RequestID = c.GetHeader(httpapi.HeaderRequestID)
	}
	return actor
}
