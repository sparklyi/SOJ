package db

import (
	"strings"
	"testing"
)

func TestTerminalStatusUpdatesAreGuardedInSQL(t *testing.T) {
	terminalGuard := "status NOT IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'system_error', 'canceled')"
	for name, query := range map[string]string{
		"UpdateSubmissionStatus": updateSubmissionStatus,
		"UpdateRunStatus":        updateRunStatus,
	} {
		if !strings.Contains(query, terminalGuard) {
			t.Fatalf("%s missing terminal guard:\n%s", name, query)
		}
	}
}

func TestResetStaleJudgeTasksSQLResetsTasksAndSubmissions(t *testing.T) {
	for _, want := range []string{
		"UPDATE judge_tasks",
		"status = 'pending'",
		"WHERE judge_tasks.status IN ('dispatching', 'running')",
		"UPDATE submissions",
		"status = 'queued'",
		"submissions.status = 'running'",
	} {
		if !strings.Contains(resetStaleJudgeTasks, want) {
			t.Fatalf("ResetStaleJudgeTasks missing %q:\n%s", want, resetStaleJudgeTasks)
		}
	}
}

func TestJudgeTaskDispatchSQLHasStrictClaimAndMarkBoundaries(t *testing.T) {
	for name, query := range map[string]string{
		"ClaimPendingJudgeTasks":     claimPendingJudgeTasks,
		"UpdateJudgeTaskDispatching": updateJudgeTaskDispatching,
		"MarkJudgeTaskDispatched":    markJudgeTaskDispatched,
		"MarkJudgeTaskRunning":       markJudgeTaskRunning,
		"RetryJudgeTask":             retryJudgeTask,
		"MarkJudgeTaskDead":          markJudgeTaskDead,
		"MarkJudgeTaskDone":          markJudgeTaskDone,
	} {
		if !strings.Contains(query, "status") {
			t.Fatalf("%s missing status condition/update:\n%s", name, query)
		}
	}
	for _, want := range []string{
		"UPDATE judge_tasks",
		"WHERE status = 'pending'",
		"AND next_run_at <= now()",
		"FOR UPDATE SKIP LOCKED",
	} {
		if !strings.Contains(claimPendingJudgeTasks, want) {
			t.Fatalf("ClaimPendingJudgeTasks missing %q:\n%s", want, claimPendingJudgeTasks)
		}
	}
	for name, query := range map[string]string{
		"MarkJudgeTaskDispatched": markJudgeTaskDispatched,
		"MarkJudgeTaskRunning":    markJudgeTaskRunning,
		"RetryJudgeTask":          retryJudgeTask,
		"MarkJudgeTaskDead":       markJudgeTaskDead,
		"MarkJudgeTaskDone":       markJudgeTaskDone,
	} {
		if !strings.Contains(query, "AND status") {
			t.Fatalf("%s missing status guard:\n%s", name, query)
		}
	}
	if !strings.Contains(markJudgeTaskDispatched, "status IN ('dispatching', 'running')") {
		t.Fatalf("MarkJudgeTaskDispatched should tolerate already-running tasks:\n%s", markJudgeTaskDispatched)
	}
	if !strings.Contains(markJudgeTaskDispatched, "WHEN status = 'dispatching' THEN 'dispatched'") {
		t.Fatalf("MarkJudgeTaskDispatched should not regress running tasks to dispatched:\n%s", markJudgeTaskDispatched)
	}
	if !strings.Contains(markJudgeTaskRunning, "status IN ('dispatching', 'dispatched', 'running')") {
		t.Fatalf("MarkJudgeTaskRunning should accept dispatching tasks:\n%s", markJudgeTaskRunning)
	}
}
