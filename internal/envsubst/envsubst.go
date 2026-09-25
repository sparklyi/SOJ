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
	expanded.Grow(len(data))

	for _, line := range strings.SplitAfter(data, "\n") {
		code, comment := splitComment(line)
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
// comment, which starts at the first # outside quotes.
func splitComment(line string) (code, comment string) {
	var quote byte

	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return line[:i], line[i:]
		}
	}
	return line, ""
}
