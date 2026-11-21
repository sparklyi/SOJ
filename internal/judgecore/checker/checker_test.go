package checker

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuiltinCheckerPolicies(t *testing.T) {
	tests := []struct {
		name     string
		policy   Policy
		expected string
		actual   string
		accepted bool
	}{
		{name: "exact", policy: PolicyExact, expected: "42\n", actual: "42\n", accepted: true},
		{name: "exact rejects whitespace", policy: PolicyExact, expected: "42\n", actual: "42 \n", accepted: false},
		{name: "trailing", policy: PolicyIgnoreTrailing, expected: "42\n", actual: "42 \n", accepted: true},
		{name: "all whitespace", policy: PolicyIgnoreAllWhitespace, expected: "4 2\n", actual: "4\n2\n", accepted: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Builtin{}.Check(context.Background(), Request{Expected: tt.expected, Actual: tt.actual, Policy: tt.policy})
			if err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			if result.Accepted != tt.accepted {
				t.Fatalf("Accepted = %v, want %v; result=%+v", result.Accepted, tt.accepted, result)
			}
		})
	}
}

func TestBuiltinCheckerPreview(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		preview string
	}{
		{"short", "hello", "hello"},
		{"ASCII limit", strings.Repeat("a", 81), strings.Repeat("a", 80)},
		{"two-byte crossing", strings.Repeat("a", 79) + "é", strings.Repeat("a", 79)},
		{"CJK crossing", strings.Repeat("a", 79) + "界", strings.Repeat("a", 79)},
		{"CJK crossing by two bytes", strings.Repeat("a", 78) + "界", strings.Repeat("a", 78)},
		{"emoji crossing", strings.Repeat("a", 77) + "😀", strings.Repeat("a", 77)},
		{"emoji crossing by two bytes", strings.Repeat("a", 78) + "😀", strings.Repeat("a", 78)},
		{"emoji crossing by three bytes", strings.Repeat("a", 79) + "😀", strings.Repeat("a", 79)},
		{"CJK at limit", strings.Repeat("a", 77) + "界!", strings.Repeat("a", 77) + "界"},
		{"emoji at limit", strings.Repeat("a", 76) + "😀!", strings.Repeat("a", 76) + "😀"},
		{"newlines", "hello\nworld", "hello\\nworld"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, expectedSide := range []bool{true, false} {
				request := Request{Expected: tt.input, Actual: "different", Policy: PolicyExact}
				want := `expected "` + tt.preview + `", got "different"`
				if !expectedSide {
					request.Expected, request.Actual = request.Actual, request.Expected
					want = `expected "different", got "` + tt.preview + `"`
				}
				result, err := (Builtin{}).Check(context.Background(), request)
				if err != nil {
					t.Fatal(err)
				}
				if result.Accepted {
					t.Fatal("mismatched output was accepted")
				}
				if !utf8.ValidString(result.DiffSummary) {
					t.Fatalf("invalid UTF-8 summary: %q", result.DiffSummary)
				}
				if result.DiffSummary != want {
					t.Fatalf("summary = %q, want %q", result.DiffSummary, want)
				}
			}
		})
	}
}

func TestBuiltinCheckerComparesBeyondPreview(t *testing.T) {
	prefix := strings.Repeat("界", 30)
	for _, actual := range []string{prefix, prefix + "!"} {
		result, err := (Builtin{}).Check(context.Background(), Request{Expected: prefix, Actual: actual, Policy: PolicyExact})
		if err != nil {
			t.Fatal(err)
		}
		if result.Accepted != (actual == prefix) {
			t.Fatalf("unexpected verdict: %+v", result)
		}
	}
}
