package db

import (
	"strings"
	"testing"
)

func TestListContestTerminalSubmissionsUsesCompleteStatusesAndSubmissionOrder(t *testing.T) {
	for _, want := range []string{
		"'output_limit'",
		"ORDER BY submitted_at, id",
	} {
		if !strings.Contains(listContestTerminalSubmissions, want) {
			t.Fatalf("ListContestTerminalSubmissions missing %q:\n%s", want, listContestTerminalSubmissions)
		}
	}
}
