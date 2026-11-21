package db

import (
	"strings"
	"testing"
)

func TestListContestTerminalSubmissionsUsesEffectiveVerdictAndSubmissionOrder(t *testing.T) {
	for _, want := range []string{
		"a.status",
		"JOIN judge_attempts a ON a.id = s.effective_attempt_id",
		"s.status = 'judged'",
		"ORDER BY s.submitted_at, s.id",
	} {
		if !strings.Contains(listContestTerminalSubmissions, want) {
			t.Fatalf("ListContestTerminalSubmissions missing %q:\n%s", want, listContestTerminalSubmissions)
		}
	}
}
