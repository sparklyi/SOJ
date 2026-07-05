package judge

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestUnavailableEngineReturnsStableProtocolErrors(t *testing.T) {
	engine := NewUnavailableEngine("agent://local")

	_, judgeErr := engine.Judge(context.Background(), Request{LanguageID: 71, Source: []byte("package main")})
	if judgeErr == nil {
		t.Fatal("Judge returned nil error")
	}
	if got, want := judgeErr.Error(), "judge endpoint agent://local is not implemented"; got != want {
		t.Fatalf("Judge error = %q, want %q", got, want)
	}

	languages, languagesErr := engine.Languages(context.Background())
	if languagesErr != nil {
		t.Fatalf("Languages returned error: %v", languagesErr)
	}
	if len(languages) != 0 {
		t.Fatalf("languages = %+v, want empty list for unavailable protocol stub", languages)
	}
}

func TestAgentRequestUsesStableProtocolShape(t *testing.T) {
	request := NewAgentRequest(Request{
		LanguageID: 71,
		Source:     []byte("package main"),
		Stdin:      "1 2\n",
		Timeout:    2 * time.Second,
		Testcases: []Testcase{{
			InputKey:          "cases/1.in",
			ExpectedOutputKey: "cases/1.out",
			TimeLimit:         1500 * time.Millisecond,
			MemoryKB:          262144,
		}},
	})

	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload["protocol_version"] != ProtocolVersion {
		t.Fatalf("protocol_version = %v, want %s", payload["protocol_version"], ProtocolVersion)
	}
	if payload["language_id"] != float64(71) {
		t.Fatalf("language_id = %v, want 71", payload["language_id"])
	}
	if payload["timeout_ms"] != float64(2000) {
		t.Fatalf("timeout_ms = %v, want 2000", payload["timeout_ms"])
	}
	testcases, ok := payload["testcases"].([]any)
	if !ok || len(testcases) != 1 {
		t.Fatalf("testcases = %#v, want one testcase", payload["testcases"])
	}
	testcase, ok := testcases[0].(map[string]any)
	if !ok {
		t.Fatalf("testcase = %#v", testcases[0])
	}
	if testcase["input_key"] != "cases/1.in" || testcase["expected_output_key"] != "cases/1.out" {
		t.Fatalf("testcase keys = %#v", testcase)
	}
}

func TestAgentResultConvertsToJudgeResult(t *testing.T) {
	result := AgentResult{
		ProtocolVersion: ProtocolVersion,
		Verdict:         VerdictWrongAnswer,
		TimeMS:          12,
		MemoryKB:        256,
		Stdout:          "actual\n",
		Stderr:          "stderr\n",
		CompileOutput:   "compile\n",
		ErrorMessage:    "first case failed",
	}

	got := result.ToResult()
	if got.Verdict != VerdictWrongAnswer || got.TimeMS != 12 || got.MemoryKB != 256 {
		t.Fatalf("result = %+v", got)
	}
	if got.Stdout != "actual\n" || got.Stderr != "stderr\n" || got.CompileOutput != "compile\n" || got.ErrorMessage != "first case failed" {
		t.Fatalf("result output fields = %+v", got)
	}
}
