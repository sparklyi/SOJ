package judge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJudge0SubmitsTestcaseInputAndExpectedOutput(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("base64_encoded") != "true" || r.URL.Query().Get("wait") != "true" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		bodies = append(bodies, body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stdout": base64.StdEncoding.EncodeToString([]byte("ok\n")),
			"time":   "0.001",
			"memory": 128,
			"status": map[string]any{"id": 3, "description": "Accepted"},
		})
	}))
	defer server.Close()

	client := NewJudge0Client(server.URL, server.Client(), "")
	_, err := client.Judge(context.Background(), Request{
		LanguageID: 71,
		Source:     []byte("package main"),
		Testcases:  []Testcase{{InputKey: "1 2\n", ExpectedOutputKey: "3\n"}},
	})
	if err != nil {
		t.Fatalf("Judge returned error: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("request count = %d", len(bodies))
	}
	if got := decodeBase64String(t, bodies[0]["stdin"]); got != "1 2\n" {
		t.Fatalf("stdin = %q", got)
	}
	if got := decodeBase64String(t, bodies[0]["expected_output"]); got != "3\n" {
		t.Fatalf("expected_output = %q", got)
	}
}

func decodeBase64String(t *testing.T, value any) string {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Fatalf("base64 value = %T %v", value, value)
	}
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		t.Fatalf("decode base64 %q: %v", value, err)
	}
	return string(decoded)
}
