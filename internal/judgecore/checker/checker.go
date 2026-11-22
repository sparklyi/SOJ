package checker

import (
	"context"
	"strings"
	"unicode/utf8"
)

type Policy string

const (
	PolicyExact               Policy = "exact"
	PolicyIgnoreTrailing      Policy = "ignore_trailing_whitespace"
	PolicyIgnoreAllWhitespace Policy = "ignore_all_whitespace"
)

type Request struct {
	Expected string
	Actual   string
	Policy   Policy
}

type Result struct {
	Accepted    bool
	Message     string
	DiffSummary string
}

type Checker interface {
	Check(ctx context.Context, request Request) (Result, error)
}

type Builtin struct{}

func (Builtin) Check(ctx context.Context, request Request) (Result, error) {
	expected := request.Expected
	actual := request.Actual
	switch request.Policy {
	case "", PolicyExact:
	case PolicyIgnoreTrailing:
		expected = trimTrailingSpaceLines(expected)
		actual = trimTrailingSpaceLines(actual)
	case PolicyIgnoreAllWhitespace:
		expected = strings.Join(strings.Fields(expected), " ")
		actual = strings.Join(strings.Fields(actual), " ")
	}
	if expected == actual {
		return Result{Accepted: true}, nil
	}
	return Result{Message: "output differs from expected output", DiffSummary: firstDiff(expected, actual)}, nil
}

func trimTrailingSpaceLines(value string) string {
	// Fast path: scanning for a dirty line does not allocate, so already
	// normalized output is returned as-is instead of being split and rejoined.
	rest := value
	for len(rest) > 0 {
		line := rest
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			line, rest = rest[:i], rest[i+1:]
		} else {
			rest = ""
		}
		if strings.TrimRight(line, " \t\r") != line {
			lines := strings.Split(value, "\n")
			for i := range lines {
				lines[i] = strings.TrimRight(lines[i], " \t\r")
			}
			return strings.Join(lines, "\n")
		}
	}
	return value
}

func firstDiff(expected, actual string) string {
	return "expected " + quoteSummary(preview(expected)) + ", got " + quoteSummary(preview(actual))
}

func preview(value string) string {
	const limit = 80
	var out strings.Builder
	out.Grow(min(len(value), limit))
	// Ranging replaces malformed bytes with RuneError. Count encoded bytes so
	// those replacements also fit within the preview limit.
	for _, r := range value {
		if out.Len()+utf8.RuneLen(r) > limit {
			break
		}
		out.WriteRune(r)
	}
	return out.String()
}

func quoteSummary(value string) string {
	value = strings.ReplaceAll(value, "\n", "\\n")
	return `"` + value + `"`
}
