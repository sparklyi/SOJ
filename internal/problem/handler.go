package problem

import (
	"io"
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

func (h *Handler) createProblem(c *gin.Context) {
	var req CreateProblemInput
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.Error(c, apperror.BadRequest("request.invalid", "invalid request body"))
		return
	}
	problem, err := h.service.CreateProblem(c.Request.Context(), actorFromContext(c), req)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	response, err := h.service.ProblemResponse(c.Request.Context(), problem)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.Created(c, response)
}

func (h *Handler) listProblems(c *gin.Context) {
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
	list, err := h.service.ListProblems(c.Request.Context(), actorFromContext(c), ListProblemsFilter{
		Difficulty: c.Query("difficulty"),
		Status:     c.Query("status"),
		Visibility: c.Query("visibility"),
		Tag:        c.Query("tag"),
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

func (h *Handler) getProblem(c *gin.Context) {
	id, ok := problemIDParam(c)
	if !ok {
		return
	}
	problem, err := h.service.GetProblem(c.Request.Context(), actorFromContext(c), id)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	response, err := h.service.ProblemResponse(c.Request.Context(), problem)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, response)
}

func (h *Handler) updateProblem(c *gin.Context) {
	id, ok := problemIDParam(c)
	if !ok {
		return
	}
	var req UpdateProblemInput
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.Error(c, apperror.BadRequest("request.invalid", "invalid request body"))
		return
	}
	problem, err := h.service.UpdateProblem(c.Request.Context(), actorFromContext(c), id, req)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	response, err := h.service.ProblemResponse(c.Request.Context(), problem)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, response)
}

func (h *Handler) archiveProblem(c *gin.Context) {
	id, ok := problemIDParam(c)
	if !ok {
		return
	}
	_, err := h.service.ArchiveProblem(c.Request.Context(), actorFromContext(c), id)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.NoContent(c)
}

func (h *Handler) createStatement(c *gin.Context) {
	id, ok := problemIDParam(c)
	if !ok {
		return
	}
	var req CreateStatementInput
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.Error(c, apperror.BadRequest("request.invalid", "invalid request body"))
		return
	}
	statement, err := h.service.CreateStatement(c.Request.Context(), actorFromContext(c), id, req)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.Created(c, statement)
}

func (h *Handler) currentStatement(c *gin.Context) {
	id, ok := problemIDParam(c)
	if !ok {
		return
	}
	statement, err := h.service.CurrentStatement(c.Request.Context(), actorFromContext(c), id)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, statement)
}

func (h *Handler) assignTags(c *gin.Context) {
	id, ok := problemIDParam(c)
	if !ok {
		return
	}
	var req AssignTagsInput
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.Error(c, apperror.BadRequest("request.invalid", "invalid request body"))
		return
	}
	tags, err := h.service.AssignTags(c.Request.Context(), actorFromContext(c), id, req)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, tags)
}

func (h *Handler) uploadTestcases(c *gin.Context) {
	id, ok := problemIDParam(c)
	if !ok {
		return
	}
	file, header, err := c.Request.FormFile("archive")
	if err != nil {
		httpapi.Error(c, testcaseNotReady("archive file is required"))
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		httpapi.Error(c, apperror.BadRequest("testcase.archive_read_failed", "failed to read archive"))
		return
	}
	caseCount64, err := strconv.ParseInt(c.PostForm("case_count"), 10, 32)
	if err != nil {
		httpapi.Error(c, testcaseNotReady("case_count must be an integer"))
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/zip"
	}
	set, err := h.service.UploadTestcaseArchive(c.Request.Context(), actorFromContext(c), id, UploadTestcaseInput{
		Content:        content,
		CaseCount:      int32(caseCount64),
		ChecksumSHA256: firstNonEmpty(c.PostForm("checksum_sha256"), c.PostForm("sha256")),
		ContentType:    contentType,
	})
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.Created(c, set)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (h *Handler) stats(c *gin.Context) {
	id, ok := problemIDParam(c)
	if !ok {
		return
	}
	stats, err := h.service.Stats(c.Request.Context(), actorFromContext(c), id)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, stats)
}

func problemIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		httpapi.Error(c, apperror.BadRequest("problem.id_invalid", "problem id is invalid"))
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
