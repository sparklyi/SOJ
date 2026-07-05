package events

import (
	"fmt"
	"time"

	"SOJ/internal/judge"
)

const (
	RequestEventType    = "judge.request.v1"
	ProgressEventType   = "judge.progress.v1"
	ResultEventType     = "judge.result.v1"
	DeadLetterEventType = "judge.dead_letter.v1"
)

type ArtifactRef struct {
	ID          int64  `json:"id"`
	StorageKey  string `json:"storage_key"`
	ContentHash string `json:"content_hash"`
}

type TestcaseSetRef struct {
	ID   int64  `json:"id"`
	Hash string `json:"hash"`
}

type TestcaseRef struct {
	Index             int    `json:"index"`
	GroupName         string `json:"group_name,omitempty"`
	InputKey          string `json:"input_key"`
	ExpectedOutputKey string `json:"expected_output_key"`
	TimeLimitMS       int64  `json:"time_limit_ms,omitempty"`
	MemoryKB          int64  `json:"memory_kb,omitempty"`
	Score             int32  `json:"score,omitempty"`
}

type RequestEvent struct {
	ProtocolVersion string         `json:"protocol_version"`
	EventID         string         `json:"event_id"`
	AttemptID       string         `json:"attempt_id"`
	TraceID         string         `json:"trace_id"`
	SubmissionID    int64          `json:"submission_id,omitempty"`
	RunID           int64          `json:"run_id,omitempty"`
	LanguageID      int64          `json:"language_id"`
	LanguageSlug    string         `json:"language_slug,omitempty"`
	SourceArtifact  ArtifactRef    `json:"source_artifact"`
	TestcaseSet     TestcaseSetRef `json:"testcase_set"`
	Testcases       []TestcaseRef  `json:"testcases,omitempty"`
	TimeoutMS       int64          `json:"timeout_ms,omitempty"`
	MemoryKB        int64          `json:"memory_kb,omitempty"`
	Priority        string         `json:"priority,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

func (e RequestEvent) Validate() error {
	if e.ProtocolVersion != "" && e.ProtocolVersion != RequestEventType {
		return fmt.Errorf("protocol_version must be %s", RequestEventType)
	}
	if e.EventID == "" {
		return fmt.Errorf("event_id is required")
	}
	if e.AttemptID == "" {
		return fmt.Errorf("attempt_id is required")
	}
	if e.TraceID == "" {
		return fmt.Errorf("trace_id is required")
	}
	if e.SubmissionID == 0 && e.RunID == 0 {
		return fmt.Errorf("submission_id or run_id is required")
	}
	if e.LanguageID == 0 {
		return fmt.Errorf("language_id is required")
	}
	if e.SourceArtifact.ID == 0 {
		return fmt.Errorf("source_artifact.id is required")
	}
	if e.SourceArtifact.StorageKey == "" {
		return fmt.Errorf("source_artifact.storage_key is required")
	}
	if e.SourceArtifact.ContentHash == "" {
		return fmt.Errorf("source_artifact.content_hash is required")
	}
	if e.TestcaseSet.ID == 0 {
		return fmt.Errorf("testcase_set.id is required")
	}
	if e.TestcaseSet.Hash == "" {
		return fmt.Errorf("testcase_set.hash is required")
	}
	return nil
}

type ProgressEvent struct {
	ProtocolVersion string    `json:"protocol_version"`
	EventID         string    `json:"event_id"`
	RequestEventID  string    `json:"request_event_id"`
	AttemptID       string    `json:"attempt_id"`
	TraceID         string    `json:"trace_id"`
	Phase           string    `json:"phase"`
	CreatedAt       time.Time `json:"created_at"`
}

func (e ProgressEvent) Validate() error {
	if e.ProtocolVersion != "" && e.ProtocolVersion != ProgressEventType {
		return fmt.Errorf("protocol_version must be %s", ProgressEventType)
	}
	return validateResponseIdentity(e.EventID, e.RequestEventID, e.AttemptID, e.TraceID)
}

type ResultEvent struct {
	ProtocolVersion string        `json:"protocol_version"`
	EventID         string        `json:"event_id"`
	RequestEventID  string        `json:"request_event_id"`
	AttemptID       string        `json:"attempt_id"`
	TraceID         string        `json:"trace_id"`
	Status          judge.Verdict `json:"status"`
	Result          judge.Result  `json:"result"`
	ErrorClass      string        `json:"error_class,omitempty"`
	Retryable       bool          `json:"retryable,omitempty"`
	SafeMessage     string        `json:"safe_message,omitempty"`
	InternalMessage string        `json:"internal_message,omitempty"`
	JudgedAt        time.Time     `json:"judged_at"`
}

func (e ResultEvent) Validate() error {
	if e.ProtocolVersion != "" && e.ProtocolVersion != ResultEventType {
		return fmt.Errorf("protocol_version must be %s", ResultEventType)
	}
	if err := validateResponseIdentity(e.EventID, e.RequestEventID, e.AttemptID, e.TraceID); err != nil {
		return err
	}
	if _, err := NormalizeVerdict(e.Status); err != nil {
		return fmt.Errorf("status: %w", err)
	}
	return nil
}

type DeadLetterEvent struct {
	ProtocolVersion string    `json:"protocol_version"`
	EventID         string    `json:"event_id"`
	RequestEventID  string    `json:"request_event_id"`
	AttemptID       string    `json:"attempt_id"`
	TraceID         string    `json:"trace_id"`
	ErrorClass      string    `json:"error_class"`
	SafeMessage     string    `json:"safe_message"`
	InternalMessage string    `json:"internal_message,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

func (e DeadLetterEvent) Validate() error {
	if e.ProtocolVersion != "" && e.ProtocolVersion != DeadLetterEventType {
		return fmt.Errorf("protocol_version must be %s", DeadLetterEventType)
	}
	if err := validateResponseIdentity(e.EventID, e.RequestEventID, e.AttemptID, e.TraceID); err != nil {
		return err
	}
	if e.ErrorClass == "" {
		return fmt.Errorf("error_class is required")
	}
	if e.SafeMessage == "" {
		return fmt.Errorf("safe_message is required")
	}
	return nil
}

func validateResponseIdentity(eventID, requestEventID, attemptID, traceID string) error {
	if eventID == "" {
		return fmt.Errorf("event_id is required")
	}
	if requestEventID == "" {
		return fmt.Errorf("request_event_id is required")
	}
	if attemptID == "" {
		return fmt.Errorf("attempt_id is required")
	}
	if traceID == "" {
		return fmt.Errorf("trace_id is required")
	}
	return nil
}

func NormalizeVerdict(verdict judge.Verdict) (judge.Verdict, error) {
	switch verdict {
	case judge.VerdictAccepted,
		judge.VerdictWrongAnswer,
		judge.VerdictCompileError,
		judge.VerdictRuntimeError,
		judge.VerdictTimeLimit,
		judge.VerdictMemoryLimit,
		judge.VerdictOutputLimit,
		judge.VerdictSystemError,
		judge.VerdictCanceled:
		return verdict, nil
	case judge.VerdictTimeLimitExceeded:
		return judge.VerdictTimeLimit, nil
	case judge.VerdictMemoryLimitExceeded:
		return judge.VerdictMemoryLimit, nil
	default:
		return "", fmt.Errorf("unknown verdict %q", verdict)
	}
}

func NormalizeResult(result judge.Result) (judge.Result, error) {
	verdict, err := NormalizeVerdict(result.Verdict)
	if err != nil {
		return judge.Result{}, err
	}
	result.Verdict = verdict
	for i := range result.Cases {
		caseVerdict, err := NormalizeVerdict(result.Cases[i].Verdict)
		if err != nil {
			return judge.Result{}, fmt.Errorf("case %d: %w", result.Cases[i].Index, err)
		}
		result.Cases[i].Verdict = caseVerdict
	}
	return result, nil
}
