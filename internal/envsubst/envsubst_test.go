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

func TestExpandResolvesInsideQuotedScalarsAfterNodeProperties(t *testing.T) {
	t.Setenv("SOJ_TEST_PROPERTY_HASH", "expanded")

	for _, input := range []string{
		"name: &label \"prefix # ${SOJ_TEST_PROPERTY_HASH}\"\n",
		"name: !!str \"prefix # ${SOJ_TEST_PROPERTY_HASH}\"\n",
		"name: &label !!str \"prefix # ${SOJ_TEST_PROPERTY_HASH}\"\n",
	} {
		t.Run(input, func(t *testing.T) {
			expanded, err := Expand(input)
			if err != nil {
				t.Fatalf("Expand() error = %v", err)
			}
			want := strings.ReplaceAll(input, "${SOJ_TEST_PROPERTY_HASH}", "expanded")
			if expanded != want {
				t.Fatalf("Expand() = %q, want %q", expanded, want)
			}
		})
	}
}

func TestExpandResolvesQuotedFlowValuesAfterColon(t *testing.T) {
	t.Setenv("SOJ_TEST_FLOW_VALUE", "expanded")

	input := `{"name":"prefix # ${SOJ_TEST_FLOW_VALUE}"}`
	expanded, err := Expand(input)
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	want := `{"name":"prefix # expanded"}`
	if expanded != want {
		t.Fatalf("Expand() = %q, want %q", expanded, want)
	}
}

func TestExpandResolvesQuotedKeyAfterExplicitFlowMappingIndicator(t *testing.T) {
	t.Setenv("SOJ_TEST_EXPLICIT_KEY", "expanded")

	input := `{? 'prefix # ${SOJ_TEST_EXPLICIT_KEY}': value}`
	expanded, err := Expand(input)
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	want := `{? 'prefix # expanded': value}`
	if expanded != want {
		t.Fatalf("Expand() = %q, want %q", expanded, want)
	}
}

func TestExpandResolvesQuotedValueInMultilineFlowMapping(t *testing.T) {
	t.Setenv("SOJ_TEST_MULTILINE_FLOW", "expanded")

	input := "value: {\n  key:\"prefix # ${SOJ_TEST_MULTILINE_FLOW}\"\n}\n"
	expanded, err := Expand(input)
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	want := "value: {\n  key:\"prefix # expanded\"\n}\n"
	if expanded != want {
		t.Fatalf("Expand() = %q, want %q", expanded, want)
	}
}

func TestExpandResolvesInsideSingleQuotedScalarWithDoubledApostrophe(t *testing.T) {
	t.Setenv("SOJ_TEST_DOUBLED_APOSTROPHE", "demo")

	input := "name: 'it''s # ${SOJ_TEST_DOUBLED_APOSTROPHE}'\n"
	expanded, err := Expand(input)
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	want := "name: 'it''s # demo'\n"
	if expanded != want {
		t.Fatalf("Expand() = %q, want %q", expanded, want)
	}
}

func TestExpandResolvesAfterLiteralHash(t *testing.T) {
	t.Setenv("SOJ_TEST_HASH_VALUE", "demo")

	expanded, err := Expand("name: prefix#${SOJ_TEST_HASH_VALUE}\n")
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	want := "name: prefix#demo\n"
	if expanded != want {
		t.Fatalf("Expand() = %q, want %q", expanded, want)
	}
}

func TestExpandPreservesCommentAfterApostropheInPlainScalar(t *testing.T) {
	t.Setenv("SOJ_TEST_MISSING_AFTER_APOSTROPHE", "")

	input := "name: prefix#it's # ${SOJ_TEST_MISSING_AFTER_APOSTROPHE}\n"
	expanded, err := Expand(input)
	if err != nil {
		t.Fatalf("Expand() error = %v, variable in comment must not be expanded", err)
	}
	if expanded != input {
		t.Fatalf("Expand() = %q, want comment preserved as %q", expanded, input)
	}
}

func TestExpandPreservesCommentAfterPunctuationInPlainScalar(t *testing.T) {
	t.Setenv("SOJ_TEST_MISSING_AFTER_PUNCTUATION", "")

	for _, input := range []string{
		"name: prefix#well-'known # ${SOJ_TEST_MISSING_AFTER_PUNCTUATION}\n",
		"name: prefix#a?'b # ${SOJ_TEST_MISSING_AFTER_PUNCTUATION}\n",
		"name: prefix#a:'b # ${SOJ_TEST_MISSING_AFTER_PUNCTUATION}\n",
		"name: value-'x # ${SOJ_TEST_MISSING_AFTER_PUNCTUATION}\n",
	} {
		t.Run(input, func(t *testing.T) {
			expanded, err := Expand(input)
			if err != nil {
				t.Fatalf("Expand() error = %v, variable in comment must not be expanded", err)
			}
			if expanded != input {
				t.Fatalf("Expand() = %q, want comment preserved as %q", expanded, input)
			}
		})
	}
}

func TestExpandResolvesQuotedSequenceItemsAfterHyphenMarker(t *testing.T) {
	t.Setenv("SOJ_TEST_SEQUENCE_VALUE", "expanded")

	input := "items:\n- 'prefix # ${SOJ_TEST_SEQUENCE_VALUE}'\n"
	expanded, err := Expand(input)
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	want := "items:\n- 'prefix # expanded'\n"
	if expanded != want {
		t.Fatalf("Expand() = %q, want %q", expanded, want)
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
