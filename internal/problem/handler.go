package problem

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/httpapi"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service     *Service
	checkRunner problemCheckRunner
	checkGetter problemCheckGetter
}

func NewHandler(service *Service) *Handler {
	handler := &Handler{service: service}
	if service != nil {
		handler.checkRunner, _ = any(service).(problemCheckRunner)
		handler.checkGetter, _ = any(service).(problemCheckGetter)
	}
	return handler
}

type problemCheckRunner interface {
	RunProblemCheck(ctx context.Context, actor auth.Actor, problemID int64) (ProblemCheckResult, error)
}

type problemCheckGetter interface {
	GetProblemCheck(ctx context.Context, actor auth.Actor, problemID int64, checkID int64) (ProblemCheckResult, error)
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
	mine, ok := boolQuery(c, "mine", false)
	if !ok {
		httpapi.Error(c, apperror.BadRequest("request.invalid", "mine must be a boolean"))
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
		Mine:       mine,
	})
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, list)
}

func (h *Handler) listProblemsByCursor(c *gin.Context) {
	pageSize, ok := int32Query(c, "page_size", 20)
	if !ok || pageSize <= 0 || pageSize > 100 {
		httpapi.Error(c, apperror.BadRequest("request.invalid", "page_size must be between 1 and 100"))
		return
	}
	mine, ok := boolQuery(c, "mine", false)
	if !ok {
		httpapi.Error(c, apperror.BadRequest("request.invalid", "mine must be a boolean"))
		return
	}
	filter := ListProblemsFilter{
		Difficulty: c.Query("difficulty"),
		Status:     c.Query("status"),
		Visibility: c.Query("visibility"),
		Tag:        c.Query("tag"),
		Keyword:    c.Query("keyword"),
		PageSize:   pageSize,
		Mine:       mine,
	}
	if raw, ok := c.GetQuery("cursor"); ok {
		var cursor ProblemCursor
		if err := httpapi.DecodeCursor(raw, &cursor); err != nil {
			httpapi.Error(c, apperror.BadRequest("invalid_cursor", "cursor is invalid"))
			return
		}
		filter.Cursor = &cursor
	}
	page, err := h.service.ListProblemsByCursor(c.Request.Context(), actorFromContext(c), filter)
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

func (h *Handler) getProblemAuthoringState(c *gin.Context) {
	id, ok := problemIDParam(c)
	if !ok {
		return
	}
	state, err := h.service.GetProblemAuthoringState(c.Request.Context(), actorFromContext(c), id)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, state)
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

func (h *Handler) uploadTestcases(c *gin.Context) {
	h.uploadTestcasesWithRequestLimit(c, defaultMaxTestcaseUploadRequestBytes)
}

func (h *Handler) uploadTestcasesWithRequestLimit(c *gin.Context, maxRequestBytes int64) {
	id, ok := problemIDParam(c)
	if !ok {
		return
	}
	if c.Request.ContentLength > maxRequestBytes {
		httpapi.Error(c, testcaseUploadTooLarge())
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes)
	file, header, err := c.Request.FormFile("archive")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpapi.Error(c, testcaseUploadTooLarge())
			return
		}
		httpapi.Error(c, testcaseNotReady("archive file is required"))
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size > defaultMaxTestcaseArchiveBytes {
		httpapi.Error(c, testcaseUploadTooLarge())
		return
	}
	content, err := io.ReadAll(io.LimitReader(file, defaultMaxTestcaseArchiveBytes+1))
	if err != nil {
		httpapi.Error(c, apperror.BadRequest("testcase.archive_read_failed", "failed to read archive"))
		return
	}
	if len(content) > defaultMaxTestcaseArchiveBytes {
		httpapi.Error(c, testcaseUploadTooLarge())
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

func testcaseUploadTooLarge() error {
	return apperror.New("testcase.archive_too_large", "testcase archive is too large", http.StatusRequestEntityTooLarge)
}

func (h *Handler) runProblemCheck(c *gin.Context) {
	id, ok := problemIDParam(c)
	if !ok {
		return
	}
	service, ok := h.problemCheckRunner()
	if !ok {
		httpapi.Error(c, apperror.ServiceUnavailable("problem checks are not available"))
		return
	}
	result, err := service.RunProblemCheck(c.Request.Context(), actorFromContext(c), id)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.Created(c, problemCheckRunResponse(result))
}

func (h *Handler) getProblemCheck(c *gin.Context) {
	id, ok := problemIDParam(c)
	if !ok {
		return
	}
	checkID, ok := problemCheckIDParam(c)
	if !ok {
		return
	}
	service, ok := h.problemCheckGetter()
	if !ok {
		httpapi.Error(c, apperror.ServiceUnavailable("problem checks are not available"))
		return
	}
	result, err := service.GetProblemCheck(c.Request.Context(), actorFromContext(c), id, checkID)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, problemCheckRunResponse(result))
}

func (h *Handler) problemCheckRunner() (problemCheckRunner, bool) {
	if h.checkRunner != nil {
		return h.checkRunner, true
	}
	if h.service == nil {
		return nil, false
	}
	service, ok := any(h.service).(problemCheckRunner)
	return service, ok
}

func (h *Handler) problemCheckGetter() (problemCheckGetter, bool) {
	if h.checkGetter != nil {
		return h.checkGetter, true
	}
	if h.service == nil {
		return nil, false
	}
	service, ok := any(h.service).(problemCheckGetter)
	return service, ok
}

func problemCheckRunResponse(result ProblemCheckResult) ProblemCheckRun {
	run := result.Run
	if result.Findings == nil {
		run.Findings = []ProblemCheckFinding{}
	} else {
		run.Findings = result.Findings
	}
	return run
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

func problemCheckIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("check_id"), 10, 64)
	if err != nil || id <= 0 {
		httpapi.Error(c, apperror.BadRequest("problem_check.id_invalid", "problem check id is invalid"))
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

func boolQuery(c *gin.Context, key string, fallback bool) (bool, bool) {
	value := c.Query(key)
	if value == "" {
		return fallback, true
	}
	parsed, err := strconv.ParseBool(value)
	return parsed, err == nil
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
