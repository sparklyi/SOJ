package checker

import (
	"context"
	"strings"
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
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t\r")
	}
	return strings.Join(lines, "\n")
}

func firstDiff(expected, actual string) string {
	if len(expected) > 80 {
		expected = expected[:80]
	}
	if len(actual) > 80 {
		actual = actual[:80]
	}
	return "expected " + quoteSummary(expected) + ", got " + quoteSummary(actual)
}

func quoteSummary(value string) string {
	value = strings.ReplaceAll(value, "\n", "\\n")
	return `"` + value + `"`
}
