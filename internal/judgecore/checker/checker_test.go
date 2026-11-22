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
		{"malformed short", "a\xffb", "a�b"},
		{"incomplete rune", "\xe2\x82", "��"},
		{"stray continuation bytes", "\x80\x81", "��"},
		{"replacement at limit", strings.Repeat("a", 77) + "\xff!", strings.Repeat("a", 77) + "�"},
		{"replacement exceeds limit", strings.Repeat("a", 78) + "\xff!", strings.Repeat("a", 78)},
		{"long malformed output", strings.Repeat("\xff", 1000), strings.Repeat("�", 26)},
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

func TestBuiltinCheckerMalformedBytesKeepOriginalVerdict(t *testing.T) {
	tests := []struct {
		expected, actual string
		accepted         bool
	}{
		{"\xff", "\xff", true},
		{"\xff", "\xfe", false},
		{"\xff", "�", false},
	}
	for _, tt := range tests {
		result, err := (Builtin{}).Check(context.Background(), Request{Expected: tt.expected, Actual: tt.actual, Policy: PolicyExact})
		if err != nil {
			t.Fatal(err)
		}
		if result.Accepted != tt.accepted {
			t.Fatalf("Check(%q, %q) accepted = %v, want %v", tt.expected, tt.actual, result.Accepted, tt.accepted)
		}
	}
}

func TestPreviewMalformedBytesBounded(t *testing.T) {
	for padding := 0; padding <= 81; padding++ {
		for _, suffix := range []string{"\xff", "\xe2\x82", "\x80\x81", strings.Repeat("\xff", 1000)} {
			got := preview(strings.Repeat("a", padding) + suffix)
			if !utf8.ValidString(got) || len(got) > 80 {
				t.Fatalf("invalid or overlong preview: %q (%d bytes)", got, len(got))
			}
		}
	}
}

func TestTrimTrailingSpaceLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "single line clean", input: "42", want: "42"},
		{name: "clean multiline", input: "a\nb\nc\n", want: "a\nb\nc\n"},
		{name: "clean without final newline", input: "a\nb\nc", want: "a\nb\nc"},
		{name: "trailing spaces", input: "a  \nb\t\n", want: "a\nb\n"},
		{name: "trailing CR", input: "a\r\nb\r\n", want: "a\nb\n"},
		{name: "mixed trailing space tab CR", input: "a \t\r\n", want: "a\n"},
		{name: "only whitespace line", input: "a\n   \nb\n", want: "a\n\nb\n"},
		{name: "leading whitespace preserved", input: "  a\n\tb\n", want: "  a\n\tb\n"},
		{name: "interior whitespace preserved", input: "a b\tc\n", want: "a b\tc\n"},
		{name: "unicode spaces untouched", input: "a b　c\n", want: "a b　c\n"},
		{name: "dirty last line without newline", input: "a\nb  ", want: "a\nb"},
		{name: "blank lines kept", input: "\n\na\n\n", want: "\n\na\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimTrailingSpaceLines(tt.input); got != tt.want {
				t.Fatalf("trimTrailingSpaceLines(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTrimTrailingSpaceLinesCleanInputDoesNotAllocate(t *testing.T) {
	clean := strings.Repeat("some judge output line\n", 1024)
	allocs := testing.AllocsPerRun(200, func() {
		if got := trimTrailingSpaceLines(clean); got != clean {
			t.Fatalf("clean input was modified: %q", got[:40])
		}
	})
	if allocs != 0 {
		t.Fatalf("trimTrailingSpaceLines allocated %v times per run on clean input, want 0", allocs)
	}
}

func BenchmarkTrimTrailingSpaceLines(b *testing.B) {
	clean := strings.Repeat("some judge output line\n", 1024)
	dirty := strings.ReplaceAll(clean, "line", "line \t")
	b.Run("clean", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if got := trimTrailingSpaceLines(clean); got != clean {
				b.Fatal("clean input was modified")
			}
		}
	})
	b.Run("dirty", func(b *testing.B) {
		b.ReportAllocs()
		want := strings.ReplaceAll(clean, "line \t", "line")
		for i := 0; i < b.N; i++ {
			if got := trimTrailingSpaceLines(dirty); got != want {
				b.Fatal("dirty input was not normalized")
			}
		}
	})
}

func TestBuiltinCheckerIgnoreTrailingVerdictsAndSummaries(t *testing.T) {
	cleanExpected := "line one\nline two\nline three\n"
	tests := []struct {
		name     string
		expected string
		actual   string
		accepted bool
		summary  string
	}{
		{
			name:     "clean output accepted without rewriting",
			expected: cleanExpected,
			actual:   cleanExpected,
			accepted: true,
		},
		{
			name:     "trailing whitespace on every line accepted",
			expected: cleanExpected,
			actual:   "line one \t\r\nline two  \nline three\t\n",
			accepted: true,
		},
		{
			name:     "dirty expected still accepted",
			expected: "a  \nb\t\n",
			actual:   "a\nb\n",
			accepted: true,
		},
		{
			name:     "real difference rejected with summary",
			expected: cleanExpected,
			actual:   "line one\nline 2\nline three\n",
			accepted: false,
			summary:  `expected "line one\nline two\nline three\n", got "line one\nline 2\nline three\n"`,
		},
		{
			name:     "difference beyond preview limit still rejected",
			expected: strings.Repeat("a", 200) + "\n",
			actual:   strings.Repeat("a", 200) + "!\n",
			accepted: false,
			summary:  `expected "` + strings.Repeat("a", 80) + `", got "` + strings.Repeat("a", 80) + `"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Builtin{}.Check(context.Background(), Request{
				Expected: tt.expected,
				Actual:   tt.actual,
				Policy:   PolicyIgnoreTrailing,
			})
			if err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			if result.Accepted != tt.accepted {
				t.Fatalf("Accepted = %v, want %v; result=%+v", result.Accepted, tt.accepted, result)
			}
			if !tt.accepted && result.DiffSummary != tt.summary {
				t.Fatalf("DiffSummary = %q, want %q", result.DiffSummary, tt.summary)
			}
		})
	}
}
