package events

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"SOJ/internal/judge"
)

func TestTraceContextSerializesSeparatelyFromTraceID(t *testing.T) {
	event := RequestEvent{
		EventID:        "evt-request-1",
		AttemptID:      "attempt-1",
		TraceID:        "operator-trace",
		TraceContext:   TraceContext{Traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", Tracestate: "vendor=value"},
		SubmissionID:   7,
		LanguageSlug:   "go",
		SourceArtifact: ArtifactRef{ID: 4, StorageKey: "source/key", ContentHash: "sha256:abc"},
		TestcaseSet:    testTestcaseSetRef(),
		CreatedAt:      time.Unix(100, 0).UTC(),
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("Unmarshal raw event: %v", err)
	}
	if raw["trace_id"] != "operator-trace" {
		t.Fatalf("trace_id = %v, want operator trace", raw["trace_id"])
	}
	carrier, ok := raw["trace_context"].(map[string]any)
	if !ok {
		t.Fatalf("trace_context = %#v, want object", raw["trace_context"])
	}
	if carrier["traceparent"] != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" || carrier["tracestate"] != "vendor=value" {
		t.Fatalf("trace_context = %#v", carrier)
	}
	if strings.Contains(raw["trace_id"].(string), "traceparent") || strings.Contains(raw["trace_id"].(string), "00-") {
		t.Fatalf("trace_id contains serialized trace context: %q", raw["trace_id"])
	}
}

func TestRequestEventValidateAllowsAbsentOrMalformedTraceContext(t *testing.T) {
	event := RequestEvent{
		EventID:        "evt-request-1",
		AttemptID:      "attempt-1",
		TraceID:        "trace-1",
		TraceContext:   TraceContext{Traceparent: "not-a-w3c-traceparent"},
		SubmissionID:   7,
		LanguageSlug:   "go",
		SourceArtifact: ArtifactRef{ID: 4, StorageKey: "source/key", ContentHash: "sha256:abc"},
		TestcaseSet:    testTestcaseSetRef(),
		CreatedAt:      time.Unix(100, 0).UTC(),
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate returned error for malformed optional trace context: %v", err)
	}

	event.TraceContext = TraceContext{}
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate returned error for absent trace context: %v", err)
	}
}

func TestRequestEventValidateRequiresProductionIdentityAndArtifactRef(t *testing.T) {
	event := RequestEvent{
		EventID:        "evt-request-1",
		AttemptID:      "attempt-1",
		TraceID:        "trace-1",
		SubmissionID:   7,
		LanguageSlug:   "go",
		SourceArtifact: ArtifactRef{ID: 4, StorageKey: "source/key", ContentHash: "sha256:abc"},
		TestcaseSet:    testTestcaseSetRef(),
		TimeoutMS:      1000,
		CreatedAt:      time.Unix(100, 0).UTC(),
	}

	if err := event.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	event.AttemptID = ""
	if err := event.Validate(); err == nil || !strings.Contains(err.Error(), "attempt_id") {
		t.Fatalf("Validate error = %v, want attempt_id", err)
	}

	event.AttemptID = "attempt-1"
	event.SourceArtifact.ContentHash = ""
	if err := event.Validate(); err == nil || !strings.Contains(err.Error(), "source_artifact.content_hash") {
		t.Fatalf("Validate error = %v, want source artifact content hash", err)
	}
}

func testTestcaseSetRef() TestcaseSetRef {
	return TestcaseSetRef{ID: 3, ChecksumSHA256: "cases-hash", StorageKey: "cases.zip", CaseCount: 1}
}

func TestResultProgressAndDeadLetterRequireRequestEventID(t *testing.T) {
	result := ResultEvent{
		EventID:        "evt-result-1",
		RequestEventID: "evt-request-1",
		AttemptID:      "attempt-1",
		TraceID:        "trace-1",
		Status:         judge.VerdictAccepted,
		JudgedAt:       time.Unix(101, 0).UTC(),
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("result Validate returned error: %v", err)
	}
	result.RequestEventID = ""
	if err := result.Validate(); err == nil || !strings.Contains(err.Error(), "request_event_id") {
		t.Fatalf("result Validate error = %v, want request_event_id", err)
	}

	progress := ProgressEvent{
		EventID:        "evt-progress-1",
		RequestEventID: "evt-request-1",
		AttemptID:      "attempt-1",
		TraceID:        "trace-1",
		Phase:          "running",
		CreatedAt:      time.Unix(102, 0).UTC(),
	}
	if err := progress.Validate(); err != nil {
		t.Fatalf("progress Validate returned error: %v", err)
	}
	progress.RequestEventID = ""
	if err := progress.Validate(); err == nil || !strings.Contains(err.Error(), "request_event_id") {
		t.Fatalf("progress Validate error = %v, want request_event_id", err)
	}

	dead := DeadLetterEvent{
		EventID:        "evt-dead-1",
		RequestEventID: "evt-request-1",
		AttemptID:      "attempt-1",
		TraceID:        "trace-1",
		ErrorClass:     "system_error",
		SafeMessage:    "agent failed",
		CreatedAt:      time.Unix(103, 0).UTC(),
	}
	if err := dead.Validate(); err != nil {
		t.Fatalf("dead-letter Validate returned error: %v", err)
	}
	dead.RequestEventID = ""
	if err := dead.Validate(); err == nil || !strings.Contains(err.Error(), "request_event_id") {
		t.Fatalf("dead-letter Validate error = %v, want request_event_id", err)
	}
}

func TestNormalizeVerdictMapsLegacyAndCanonicalValues(t *testing.T) {
	cases := map[judge.Verdict]judge.Verdict{
		judge.VerdictAccepted:            judge.VerdictAccepted,
		judge.VerdictWrongAnswer:         judge.VerdictWrongAnswer,
		judge.VerdictCompileError:        judge.VerdictCompileError,
		judge.VerdictRuntimeError:        judge.VerdictRuntimeError,
		judge.VerdictTimeLimitExceeded:   judge.VerdictTimeLimit,
		judge.VerdictMemoryLimitExceeded: judge.VerdictMemoryLimit,
		judge.VerdictTimeLimit:           judge.VerdictTimeLimit,
		judge.VerdictMemoryLimit:         judge.VerdictMemoryLimit,
		judge.VerdictOutputLimit:         judge.VerdictOutputLimit,
		judge.VerdictSystemError:         judge.VerdictSystemError,
		judge.VerdictCanceled:            judge.VerdictCanceled,
	}
	for input, want := range cases {
		got, err := NormalizeVerdict(input)
		if err != nil {
			t.Fatalf("NormalizeVerdict(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeVerdict(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeResultRewritesAggregateAndCaseVerdicts(t *testing.T) {
	result := judge.Result{
		Verdict: judge.VerdictTimeLimitExceeded,
		Cases: []judge.CaseResult{
			{Index: 1, Verdict: judge.VerdictMemoryLimitExceeded},
			{Index: 2, Verdict: judge.VerdictWrongAnswer},
		},
	}

	got, err := NormalizeResult(result)
	if err != nil {
		t.Fatalf("NormalizeResult returned error: %v", err)
	}
	if got.Verdict != judge.VerdictTimeLimit {
		t.Fatalf("aggregate verdict = %q", got.Verdict)
	}
	if got.Cases[0].Verdict != judge.VerdictMemoryLimit || got.Cases[1].Verdict != judge.VerdictWrongAnswer {
		t.Fatalf("case verdicts = %+v", got.Cases)
	}
}

// A self-run has nothing to judge, so it carries no testcase set. Accepting that
// is what lets the agent call Run instead of Judge; requiring a testcase set
// only of submissions keeps "there is nothing to compare" from being spelled as
// an empty one.
func TestRequestEventValidateAcceptsRunWithoutTestcaseSet(t *testing.T) {
	event := RequestEvent{
		EventID:        "evt-request-1",
		AttemptID:      "attempt-1",
		TraceID:        "trace-1",
		RunID:          12,
		LanguageSlug:   "go",
		SourceArtifact: ArtifactRef{ID: 4, StorageKey: "source/key", ContentHash: "sha256:abc"},
		Stdin:          "7 8\n",
		CreatedAt:      time.Unix(100, 0).UTC(),
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate returned error for a run without a testcase set: %v", err)
	}
}

// The relaxation must be exactly as wide as the run case. A submission with no
// testcase set would be judged against nothing, which is the failure this
// validation exists to prevent.
func TestRequestEventValidateStillRequiresTestcaseSetForSubmissions(t *testing.T) {
	event := RequestEvent{
		EventID:        "evt-request-1",
		AttemptID:      "attempt-1",
		TraceID:        "trace-1",
		SubmissionID:   7,
		LanguageSlug:   "go",
		SourceArtifact: ArtifactRef{ID: 4, StorageKey: "source/key", ContentHash: "sha256:abc"},
		CreatedAt:      time.Unix(100, 0).UTC(),
	}
	if err := event.Validate(); err == nil {
		t.Fatal("Validate accepted a submission with no testcase set")
	}
}

// A run has no submission_id, and a submission has no run_id. One of them must be
// present or the result cannot be routed back to a row.
func TestRequestEventValidateRequiresASubject(t *testing.T) {
	event := RequestEvent{
		EventID:        "evt-request-1",
		AttemptID:      "attempt-1",
		TraceID:        "trace-1",
		LanguageSlug:   "go",
		SourceArtifact: ArtifactRef{ID: 4, StorageKey: "source/key", ContentHash: "sha256:abc"},
		CreatedAt:      time.Unix(100, 0).UTC(),
	}
	if err := event.Validate(); err == nil {
		t.Fatal("Validate accepted a request with neither submission_id nor run_id")
	}
}

// TestResultEventWireFormatIsTheProtocolContract pins the field names of the
// embedded judge.Result payload. That payload is the `judge.result.v1` contract
// carried over Redis Streams, so a rename here is a protocol break rather than a
// refactor: an old worker reading a new agent would decode zero values instead
// of failing loudly.
func TestResultEventWireFormatIsTheProtocolContract(t *testing.T) {
	event := ResultEvent{
		ProtocolVersion: ResultEventType,
		EventID:         "evt-result-1",
		RequestEventID:  "evt-request-1",
		AttemptID:       "attempt-1",
		TraceID:         "trace-1",
		Status:          judge.VerdictAccepted,
		Result: judge.Result{
			Verdict:  judge.VerdictAccepted,
			TimeMS:   12,
			MemoryKB: 3456,
			Cases:    []judge.CaseResult{{Index: 1, Verdict: judge.VerdictAccepted}},
			Manifest: judge.Manifest{JudgeCoreVersion: "1.0"},
			JudgedAt: time.Unix(0, 0).UTC(),
		},
		JudgedAt: time.Unix(0, 0).UTC(),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("Unmarshal raw event: %v", err)
	}

	result, ok := raw["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %v, want object", raw["result"])
	}
	assertWireKeys(t, "result", result, []string{
		"Verdict", "TimeMS", "MemoryKB", "Stdout", "Stderr",
		"CompileOutput", "ErrorMessage", "Cases", "Manifest", "JudgedAt",
	})

	cases, ok := result["Cases"].([]any)
	if !ok || len(cases) != 1 {
		t.Fatalf("result.Cases = %v, want one case", result["Cases"])
	}
	firstCase, ok := cases[0].(map[string]any)
	if !ok {
		t.Fatalf("result.Cases[0] = %v, want object", cases[0])
	}
	assertWireKeys(t, "result.Cases[0]", firstCase, []string{
		"Index", "GroupName", "TestcaseKey", "Verdict", "Score", "TimeMS",
		"MemoryKB", "ExitCode", "Signal", "CheckerMessage", "OutputDiffSummary",
	})

	manifest, ok := result["Manifest"].(map[string]any)
	if !ok {
		t.Fatalf("result.Manifest = %v, want object", result["Manifest"])
	}
	assertWireKeys(t, "result.Manifest", manifest, []string{
		"JudgeCoreVersion", "JudgeAgentID", "LanguageRuntime", "SandboxBackend",
		"SandboxProfile", "TestcaseSetHash", "CheckerHash", "ValidatorHash",
		"TraceID", "Raw",
	})
}

func assertWireKeys(t *testing.T, path string, object map[string]any, want []string) {
	t.Helper()
	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	sort.Strings(got)
	expected := slices.Clone(want)
	sort.Strings(expected)

	if !slices.Equal(got, expected) {
		t.Fatalf("%s keys = %v, want %v", path, got, expected)
	}
}
