package checker

import (
	"context"
	"testing"
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
