package envsubst

import (
	"strings"
	"testing"
)

func TestExpandResolvesEnvironmentAndDefaults(t *testing.T) {
	t.Setenv("SOJ_TEST_SET", "value")

	expanded, err := Expand("a: ${SOJ_TEST_SET}\nb: ${SOJ_TEST_UNSET:-fallback}\nc: ${SOJ_TEST_SET}\n")
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}

	want := "a: value\nb: fallback\nc: value\n"
	if expanded != want {
		t.Fatalf("Expand() = %q, want %q", expanded, want)
	}
}

func TestExpandReportsMissingVariables(t *testing.T) {
	_, err := Expand("a: ${SOJ_TEST_MISSING}\nb: ${SOJ_TEST_ALSO_MISSING}\n")
	if err == nil {
		t.Fatal("Expand() error = nil, want missing variable error")
	}
	for _, name := range []string{"SOJ_TEST_ALSO_MISSING", "SOJ_TEST_MISSING"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("Expand() error = %v, want %s named", err, name)
		}
	}
}

func TestExpandIgnoresComments(t *testing.T) {
	expanded, err := Expand("# ${SOJ_TEST_UNSET}\na: 1 # ${SOJ_TEST_ALSO_UNSET}\n")
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if !strings.Contains(expanded, "${SOJ_TEST_UNSET}") {
		t.Fatalf("Expand() = %q, comments must be left alone", expanded)
	}
}

func TestExpandResolvesInsideQuotedValues(t *testing.T) {
	t.Setenv("SOJ_TEST_HASH", "with#hash")

	expanded, err := Expand("a: \"${SOJ_TEST_HASH}\" # comment\n")
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if !strings.Contains(expanded, "with#hash") {
		t.Fatalf("Expand() = %q, want the value expanded", expanded)
	}
}

func TestExpandTreatsEmptyAsUnset(t *testing.T) {
	t.Setenv("SOJ_TEST_EMPTY", "")

	expanded, err := Expand("a: ${SOJ_TEST_EMPTY:-fallback}\n")
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if !strings.Contains(expanded, "fallback") {
		t.Fatalf("Expand() = %q, want the default", expanded)
	}

	if _, err := Expand("a: ${SOJ_TEST_EMPTY}\n"); err == nil {
		t.Fatal("Expand() error = nil, want an empty required value rejected")
	}
}
