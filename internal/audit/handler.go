package audit

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

func (h *Handler) listEvents(c *gin.Context) {
	filter, ok := listFilterFromQuery(c)
	if !ok {
		return
	}
	list, err := h.service.ListEvents(c.Request.Context(), actorFromContext(c), filter)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, list)
}

func listFilterFromQuery(c *gin.Context) (ListFilter, bool) {
	page, ok := int32Query(c, "page", 1)
	if !ok || page <= 0 {
		httpapi.Error(c, apperror.BadRequest("audit.invalid_page", "page must be a positive integer"))
		return ListFilter{}, false
	}
	pageSize, ok := int32Query(c, "page_size", 20)
	if !ok || pageSize <= 0 || pageSize > 100 {
		httpapi.Error(c, apperror.BadRequest("audit.invalid_page_size", "page_size must be between 1 and 100"))
		return ListFilter{}, false
	}
	objectID, ok := int64Query(c, "object_id")
	if !ok {
		httpapi.Error(c, apperror.BadRequest("audit.invalid_object_id", "object_id must be a positive integer"))
		return ListFilter{}, false
	}
	actorID, ok := int64Query(c, "actor_id")
	if !ok {
		httpapi.Error(c, apperror.BadRequest("audit.invalid_actor_id", "actor_id must be a positive integer"))
		return ListFilter{}, false
	}
	return ListFilter{
		ObjectType: ObjectType(c.Query("object_type")),
		ObjectID:   objectID,
		ActorID:    actorID,
		Action:     Action(c.Query("action")),
		Page:       page,
		PageSize:   pageSize,
	}, true
}

func actorFromContext(c *gin.Context) auth.Actor {
	for _, key := range []string{"actor", "auth.actor"} {
		if value, ok := c.Get(key); ok {
			if actor, ok := value.(auth.Actor); ok {
				return actor
			}
		}
	}
	return auth.Anonymous(c.GetString(httpapi.ContextRequestID))
}

func int32Query(c *gin.Context, key string, fallback int32) (int32, bool) {
	raw := c.Query(key)
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(value), true
}

func int64Query(c *gin.Context, key string) (int64, bool) {
	raw := c.Query(key)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}
