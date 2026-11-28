// Package envsubst expands ${NAME} and ${NAME:-default} references in
// configuration files against the process environment.
//
// It exists as its own package because both the runtime configuration and the
// language catalog use the same file contract: a value may name an environment
// variable, and a reference without a default must resolve. Keeping one
// implementation keeps the two files' semantics identical.
package envsubst

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

// refPattern matches ${NAME} and ${NAME:-default}.
var refPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// Expand replaces ${NAME} and ${NAME:-default} with environment values.
//
// A reference without a default must resolve to a non-empty value; anything
// else is collected and reported, so a missing secret stops the process
// instead of starting it with an empty one. Comments are left alone: a
// placeholder written in a comment documents the file, it does not configure
// it.
func Expand(data string) (string, error) {
	var missing []string
	var expanded strings.Builder
	var lex yamlLexState
	expanded.Grow(len(data))

	for _, line := range strings.SplitAfter(data, "\n") {
		code, comment := splitComment(line, &lex)
		expanded.WriteString(refPattern.ReplaceAllStringFunc(code, func(ref string) string {
			match := refPattern.FindStringSubmatch(ref)
			if value, ok := os.LookupEnv(match[1]); ok && value != "" {
				return value
			}
			if match[2] != "" {
				return match[3]
			}
			missing = append(missing, match[1])
			return ref
		}))
		expanded.WriteString(comment)
	}

	if len(missing) == 0 {
		return expanded.String(), nil
	}
	slices.Sort(missing)
	return "", fmt.Errorf("undefined environment variable(s): %s", strings.Join(slices.Compact(missing), ", "))
}

// splitComment splits a line into the part the loader expands and a trailing
// comment. As in YAML, a comment starts at a # outside quotes that is at the
// start of the line or preceded by whitespace, so a # inside a plain scalar
// such as prefix#value is part of the value.
type yamlLexState struct {
	quote     byte
	flowDepth int
}

func splitComment(line string, state *yamlLexState) (code, comment string) {
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case state.quote != 0:
			if c == '\\' && state.quote == '"' {
				i++
				continue
			}
			if c == state.quote {
				if state.quote == '\'' && i+1 < len(line) && line[i+1] == '\'' {
					i++
					continue
				}
				state.quote = 0
			}
		case (c == '"' || c == '\'') && startsQuotedScalar(line, i, state.flowDepth):
			state.quote = c
		case (c == '[' || c == '{') && startsFlowCollection(line, i):
			state.flowDepth++
		case (c == ']' || c == '}') && state.flowDepth > 0:
			state.flowDepth--
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return line[:i], line[i:]
		}
	}
	return line, ""
}

// startsFlowCollection reports whether an opening bracket begins a collection
// rather than appearing as content inside a plain scalar.
func startsFlowCollection(line string, i int) bool {
	if i == 0 {
		return true
	}
	switch line[i-1] {
	case ' ', '\t', ':', ',', '[', '{':
		return true
	default:
		return false
	}
}

// isBlank reports whether line[i] is a space or tab.
func isBlank(line string, i int) bool {
	return i >= 0 && i < len(line) && (line[i] == ' ' || line[i] == '\t')
}

// startsQuotedScalar reports whether quote opens a YAML scalar at this position.
// Quote characters inside plain scalars (for example, the apostrophe in "it's")
// are ordinary content and must not hide a later comment.
func startsQuotedScalar(line string, i, flowDepth int) bool {
	// Without whitespace, only a flow delimiter can directly precede a quoted
	// scalar. This fast path also avoids rescanning long plain-scalar tokens for
	// every apostrophe they contain.
	if i > 0 && !isBlank(line, i-1) {
		switch line[i-1] {
		case ',', '[', '{':
			return true
		case ':':
			return flowDepth > 0
		default:
			return false
		}
	}

	for i--; isBlank(line, i); i-- {
	}
	if i < 0 {
		return true
	}
	switch line[i] {
	case ',', '[', '{':
		return true
	case ':':
		return isBlank(line, i+1)
	case '-':
		return isBlank(line, i+1) && (i == 0 || isBlank(line, i-1))
	case '?':
		return isBlank(line, i+1) && (i == 0 || isBlank(line, i-1) ||
			(flowDepth > 0 && (line[i-1] == '[' || line[i-1] == '{' || line[i-1] == ',')))
	}

	// YAML node properties can precede a quoted scalar, for example
	// `&label "value"` or `!!str "value"`. Skip those property tokens
	// before deciding whether the quote starts the scalar.
	for {
		end := i + 1
		for i >= 0 && !isBlank(line, i) {
			i--
		}
		property := line[i+1 : end]
		if property[0] != '&' && property[0] != '!' {
			return false
		}
		for i >= 0 && isBlank(line, i) {
			i--
		}
		if i < 0 {
			return true
		}
		switch line[i] {
		case ',', '[', '{':
			return true
		case ':':
			return isBlank(line, i+1)
		case '-':
			return isBlank(line, i+1) && (i == 0 || isBlank(line, i-1))
		case '?':
			return isBlank(line, i+1) && (i == 0 || isBlank(line, i-1) ||
				(flowDepth > 0 && (line[i-1] == '[' || line[i-1] == '{' || line[i-1] == ',')))
		}
	}
}
