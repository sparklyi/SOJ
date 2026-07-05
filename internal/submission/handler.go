package submission

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

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

type createSubmissionRequest struct {
	ProblemID  int64  `json:"problem_id" binding:"required"`
	ContestID  *int64 `json:"contest_id"`
	LanguageID int64  `json:"language_id" binding:"required"`
	SourceCode string `json:"source_code" binding:"required"`
}

func (h *Handler) CreateSubmission(c *gin.Context) {
	var req createSubmissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.Error(c, apperror.BadRequest("invalid_request", err.Error()))
		return
	}
	out, err := h.service.CreateSubmission(c.Request.Context(), actorFromContext(c), CreateSubmissionInput{ProblemID: req.ProblemID, ContestID: req.ContestID, LanguageID: req.LanguageID, Source: []byte(req.SourceCode)})
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.Accepted(c, submissionResponse(SubmissionView{Submission: out.Submission, Visibility: "visible"}))
}

func (h *Handler) ListSubmissions(c *gin.Context) {
	page, pageSize, ok := pageQuery(c)
	if !ok {
		return
	}
	input := ListSubmissionsInput{Limit: pageSize, Offset: (page - 1) * pageSize}
	if raw := c.Query("user_id"); raw != "" {
		value, ok := int64Query(c, raw, "invalid_user_id")
		if !ok {
			return
		}
		input.UserID = &value
	}
	if raw := c.Query("problem_id"); raw != "" {
		value, ok := int64Query(c, raw, "invalid_problem_id")
		if !ok {
			return
		}
		input.ProblemID = &value
	}
	if raw := c.Query("contest_id"); raw != "" {
		value, ok := int64Query(c, raw, "invalid_contest_id")
		if !ok {
			return
		}
		input.ContestID = &value
	}
	if raw := c.Query("status"); raw != "" {
		input.Status = &raw
	}
	items, total, err := h.service.ListSubmissions(c.Request.Context(), actorFromContext(c), input)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, gin.H{"items": submissionResponses(items), "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) GetSubmission(c *gin.Context) {
	id, ok := idParam(c, "id", "invalid_submission_id")
	if !ok {
		return
	}
	out, err := h.service.GetSubmission(c.Request.Context(), actorFromContext(c), id)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, submissionResponse(out))
}

type createRunRequest struct {
	ProblemID  int64  `json:"problem_id" binding:"required"`
	LanguageID int64  `json:"language_id" binding:"required"`
	SourceCode string `json:"source_code" binding:"required"`
	Stdin      string `json:"stdin"`
}

func (h *Handler) CreateRun(c *gin.Context) {
	var req createRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.Error(c, apperror.BadRequest("invalid_request", err.Error()))
		return
	}
	out, err := h.service.CreateRun(c.Request.Context(), actorFromContext(c), CreateRunInput{ProblemID: req.ProblemID, LanguageID: req.LanguageID, Source: []byte(req.SourceCode), Stdin: req.Stdin})
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	if terminalStatus(out.Run.Status) {
		httpapi.OK(c, runResponse(out.Run))
		return
	}
	c.JSON(http.StatusAccepted, httpapi.Envelope{Data: runResponse(out.Run), RequestID: c.GetString(httpapi.ContextRequestID)})
}

func (h *Handler) GetRun(c *gin.Context) {
	id, ok := idParam(c, "id", "invalid_run_id")
	if !ok {
		return
	}
	out, err := h.service.GetRun(c.Request.Context(), actorFromContext(c), id)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, runResponse(out))
}

func (h *Handler) ListLanguages(c *gin.Context) {
	page, pageSize, ok := pageQuery(c)
	if !ok {
		return
	}
	var enabled *bool
	if raw := c.Query("enabled"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			httpapi.Error(c, apperror.BadRequest("invalid_enabled", "enabled must be a boolean"))
			return
		}
		enabled = &value
	}
	var engine *string
	if raw := c.Query("engine"); raw != "" {
		engine = &raw
	}
	items, total, err := h.service.ListLanguages(c.Request.Context(), actorFromContext(c), ListLanguagesInput{Enabled: enabled, Engine: engine, Limit: pageSize, Offset: (page - 1) * pageSize})
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, gin.H{"items": languageResponses(items), "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) SyncLanguages(c *gin.Context) {
	_, err := h.service.SyncLanguages(c.Request.Context(), actorFromContext(c))
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.AcceptedEmpty(c)
}

type updateLanguageRequest struct {
	Enabled              *bool  `json:"enabled"`
	DefaultTimeLimitMS   *int32 `json:"default_time_limit_ms"`
	DefaultMemoryLimitKB *int32 `json:"default_memory_limit_kb"`
}

func (h *Handler) UpdateLanguage(c *gin.Context) {
	id, ok := idParam(c, "id", "invalid_language_id")
	if !ok {
		return
	}
	var req updateLanguageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.Error(c, apperror.BadRequest("invalid_request", err.Error()))
		return
	}
	item, err := h.service.UpdateLanguage(c.Request.Context(), actorFromContext(c), id, UpdateLanguageInput{Enabled: req.Enabled, DefaultTimeLimitMS: req.DefaultTimeLimitMS, DefaultMemoryLimitKB: req.DefaultMemoryLimitKB})
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, languageResponse(item))
}

type submissionJSON struct {
	ID               int64                  `json:"id"`
	UserID           int64                  `json:"user_id"`
	ProblemID        int64                  `json:"problem_id"`
	ContestID        *int64                 `json:"contest_id,omitempty"`
	LanguageID       int64                  `json:"language_id"`
	Status           string                 `json:"status"`
	Score            int32                  `json:"score"`
	TimeMS           *int32                 `json:"time_ms,omitempty"`
	MemoryKB         *int32                 `json:"memory_kb,omitempty"`
	ErrorMessage     *string                `json:"error_message,omitempty"`
	SubmittedAt      time.Time              `json:"submitted_at"`
	JudgedAt         *time.Time             `json:"judged_at,omitempty"`
	UpdatedAt        time.Time              `json:"updated_at"`
	Visibility       string                 `json:"visibility,omitempty"`
	Result           *submissionResultJSON  `json:"result,omitempty"`
	Cases            []submissionCaseJSON   `json:"cases,omitempty"`
	AdminDiagnostics *submissionAttemptJSON `json:"admin_diagnostics,omitempty"`
}

type submissionResultJSON struct {
	AttemptID            int64           `json:"attempt_id"`
	Status               string          `json:"status"`
	Score                int32           `json:"score"`
	TimeMS               *int32          `json:"time_ms,omitempty"`
	MemoryKB             *int32          `json:"memory_kb,omitempty"`
	FirstFailedCaseIndex *int32          `json:"first_failed_case_index,omitempty"`
	FirstFailedGroup     *string         `json:"first_failed_group,omitempty"`
	ErrorClass           *string         `json:"error_class,omitempty"`
	SafeSummary          json.RawMessage `json:"safe_summary,omitempty"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type submissionCaseJSON struct {
	CaseIndex         int32   `json:"case_index"`
	GroupName         *string `json:"group_name,omitempty"`
	Status            string  `json:"status"`
	Score             int32   `json:"score"`
	TimeMS            *int32  `json:"time_ms,omitempty"`
	MemoryKB          *int32  `json:"memory_kb,omitempty"`
	CheckerMessage    *string `json:"checker_message,omitempty"`
	OutputDiffSummary *string `json:"output_diff_summary,omitempty"`
}

type submissionAttemptJSON struct {
	AttemptID            int64   `json:"attempt_id"`
	AttemptNo            int32   `json:"attempt_no"`
	ProtocolVersion      string  `json:"protocol_version"`
	JudgeCoreVersion     string  `json:"judge_core_version"`
	JudgeEngine          string  `json:"judge_engine"`
	JudgeAgentID         *string `json:"judge_agent_id,omitempty"`
	LanguageRuntime      *string `json:"language_runtime,omitempty"`
	SandboxBackend       *string `json:"sandbox_backend,omitempty"`
	SandboxProfile       *string `json:"sandbox_profile,omitempty"`
	TraceID              *string `json:"trace_id,omitempty"`
	CompileOutputSummary *string `json:"compile_output_summary,omitempty"`
	StderrSummary        *string `json:"stderr_summary,omitempty"`
	ErrorClass           *string `json:"error_class,omitempty"`
	ErrorMessage         *string `json:"error_message,omitempty"`
}

func submissionResponse(view SubmissionView) submissionJSON {
	record := view.Submission
	out := submissionJSON{
		ID:           record.ID,
		UserID:       record.UserID,
		ProblemID:    record.ProblemID,
		ContestID:    record.ContestID,
		LanguageID:   record.LanguageID,
		Status:       record.Status,
		Score:        record.Score,
		TimeMS:       record.TimeMS,
		MemoryKB:     record.MemoryKB,
		ErrorMessage: record.ErrorMessage,
		SubmittedAt:  record.SubmittedAt,
		JudgedAt:     record.JudgedAt,
		UpdatedAt:    record.UpdatedAt,
		Visibility:   view.Visibility,
	}
	if view.Result != nil {
		out.Result = &submissionResultJSON{
			AttemptID:            view.Result.AttemptID,
			Status:               view.Result.Status,
			Score:                view.Result.Score,
			TimeMS:               view.Result.TimeMS,
			MemoryKB:             view.Result.MemoryKB,
			FirstFailedCaseIndex: view.Result.FirstFailedCaseIndex,
			FirstFailedGroup:     view.Result.FirstFailedGroup,
			ErrorClass:           view.Result.ErrorClass,
			SafeSummary:          json.RawMessage(view.Result.SafeSummary),
			UpdatedAt:            view.Result.UpdatedAt,
		}
	}
	for _, item := range view.Cases {
		out.Cases = append(out.Cases, submissionCaseJSON{
			CaseIndex:         item.CaseIndex,
			GroupName:         item.GroupName,
			Status:            item.Status,
			Score:             item.Score,
			TimeMS:            item.TimeMS,
			MemoryKB:          item.MemoryKB,
			CheckerMessage:    item.CheckerMessage,
			OutputDiffSummary: item.OutputDiffSummary,
		})
	}
	if view.AdminDiagnostics != nil {
		out.AdminDiagnostics = &submissionAttemptJSON{
			AttemptID:            view.AdminDiagnostics.ID,
			AttemptNo:            view.AdminDiagnostics.AttemptNo,
			ProtocolVersion:      view.AdminDiagnostics.ProtocolVersion,
			JudgeCoreVersion:     view.AdminDiagnostics.JudgeCoreVersion,
			JudgeEngine:          view.AdminDiagnostics.JudgeEngine,
			JudgeAgentID:         view.AdminDiagnostics.JudgeAgentID,
			LanguageRuntime:      view.AdminDiagnostics.LanguageRuntime,
			SandboxBackend:       view.AdminDiagnostics.SandboxBackend,
			SandboxProfile:       view.AdminDiagnostics.SandboxProfile,
			TraceID:              view.AdminDiagnostics.TraceID,
			CompileOutputSummary: view.AdminDiagnostics.CompileOutputSummary,
			StderrSummary:        view.AdminDiagnostics.StderrSummary,
			ErrorClass:           view.AdminDiagnostics.ErrorClass,
			ErrorMessage:         view.AdminDiagnostics.ErrorMessage,
		}
	}
	return out
}

func submissionResponses(views []SubmissionView) []submissionJSON {
	out := make([]submissionJSON, 0, len(views))
	for _, view := range views {
		out = append(out, submissionResponse(view))
	}
	return out
}

type runJSON struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"user_id"`
	ProblemID     int64      `json:"problem_id"`
	LanguageID    int64      `json:"language_id"`
	Status        string     `json:"status"`
	Stdout        string     `json:"stdout,omitempty"`
	Stderr        string     `json:"stderr,omitempty"`
	CompileOutput string     `json:"compile_output,omitempty"`
	TimeMS        *int32     `json:"time_ms,omitempty"`
	MemoryKB      *int32     `json:"memory_kb,omitempty"`
	ErrorMessage  *string    `json:"error_message,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func runResponse(record RunRecord) runJSON {
	return runJSON{
		ID:            record.ID,
		UserID:        record.UserID,
		ProblemID:     record.ProblemID,
		LanguageID:    record.LanguageID,
		Status:        record.Status,
		Stdout:        record.Stdout,
		Stderr:        record.Stderr,
		CompileOutput: record.CompileOutput,
		TimeMS:        record.TimeMS,
		MemoryKB:      record.MemoryKB,
		ErrorMessage:  record.ErrorMessage,
		CreatedAt:     record.CreatedAt,
		FinishedAt:    record.FinishedAt,
		UpdatedAt:     record.UpdatedAt,
	}
}

type languageJSON struct {
	ID                   int64  `json:"id"`
	Engine               string `json:"engine"`
	EngineLanguageID     string `json:"engine_language_id"`
	Name                 string `json:"name"`
	DefaultTimeLimitMS   int64  `json:"default_time_limit_ms"`
	DefaultMemoryLimitKB int64  `json:"default_memory_limit_kb"`
	Enabled              bool   `json:"enabled"`
}

func languageResponse(record LanguageRecord) languageJSON {
	return languageJSON{
		ID:                   record.ID,
		Engine:               record.Engine,
		EngineLanguageID:     record.EngineLanguageID,
		Name:                 record.Name,
		DefaultTimeLimitMS:   int64(record.DefaultTimeLimit / time.Millisecond),
		DefaultMemoryLimitKB: record.DefaultMemoryKB,
		Enabled:              record.Enabled,
	}
}

func languageResponses(records []LanguageRecord) []languageJSON {
	out := make([]languageJSON, 0, len(records))
	for _, record := range records {
		out = append(out, languageResponse(record))
	}
	return out
}

func idParam(c *gin.Context, name string, code string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		httpapi.Error(c, apperror.BadRequest(code, "id must be a positive integer"))
		return 0, false
	}
	return id, true
}

func int64Query(c *gin.Context, raw string, code string) (int64, bool) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		httpapi.Error(c, apperror.BadRequest(code, "query value must be a positive integer"))
		return 0, false
	}
	return value, true
}

func pageQuery(c *gin.Context) (int32, int32, bool) {
	page := int32(1)
	if raw := c.Query("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpapi.Error(c, apperror.BadRequest("invalid_page", "page must be a positive integer"))
			return 0, 0, false
		}
		page = int32(parsed)
	}
	pageSize := int32(20)
	if raw := c.Query("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 100 {
			httpapi.Error(c, apperror.BadRequest("invalid_page_size", "page_size must be between 1 and 100"))
			return 0, 0, false
		}
		pageSize = int32(parsed)
	}
	return page, pageSize, true
}

func actorFromContext(c *gin.Context) auth.Actor {
	if value, ok := c.Get("actor"); ok {
		if actor, ok := value.(auth.Actor); ok {
			return actor
		}
	}
	userID, _ := strconv.ParseInt(c.GetHeader("X-User-ID"), 10, 64)
	role, _ := auth.ParseRole(c.GetHeader("X-User-Role"))
	return auth.Actor{UserID: userID, Role: role, RequestID: c.GetString(httpapi.ContextRequestID)}
}
