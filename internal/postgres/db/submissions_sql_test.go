package db

import (
	"os"
	"path/filepath"
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

func TestJudgeResultSchemaHasDurableAttemptAndProjectionTables(t *testing.T) {
	schema := readInitialSchema(t)
	for _, want := range []string{
		"CREATE TABLE judge_attempts",
		"CREATE TABLE judge_case_results",
		"CHECK ((submission_id IS NOT NULL)::int + (run_id IS NOT NULL)::int = 1)",
		"CREATE UNIQUE INDEX judge_attempts_submission_attempt_uidx",
		"CREATE UNIQUE INDEX judge_attempts_run_attempt_uidx",
		"CREATE UNIQUE INDEX judge_case_results_attempt_case_uidx",
		"submission_id bigint NOT NULL REFERENCES submissions(id) ON DELETE CASCADE",
		"attempt_id bigint NOT NULL REFERENCES judge_attempts(id) ON DELETE CASCADE",
		"PRIMARY KEY (submission_id)",
		"safe_summary jsonb NOT NULL DEFAULT '{}'::jsonb",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("initial schema missing %q", want)
		}
	}
	submissionResultsSchema := tableSchema(t, schema, "submission_results", "contest_problem_results")
	if strings.Contains(submissionResultsSchema, "details jsonb NOT NULL DEFAULT '{}'::jsonb") {
		t.Fatalf("submission_results should not keep generic details jsonb")
	}
}

func TestArtifactSchemaSupportsJudgeArtifacts(t *testing.T) {
	schema := readInitialSchema(t)
	for _, want := range []string{
		"'judge_attempt'",
		"'judge_case'",
		"'manifest'",
		"'case_stdout'",
		"'case_stderr'",
		"'output_diff'",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("artifact schema missing %q", want)
		}
	}
}

func TestContestProjectionReferencesJudgeAttempts(t *testing.T) {
	schema := readInitialSchema(t)
	for _, want := range []string{
		"best_submission_id bigint REFERENCES submissions(id)",
		"best_attempt_id bigint REFERENCES judge_attempts(id)",
		"last_attempt_id bigint REFERENCES judge_attempts(id)",
		"CREATE INDEX contest_problem_results_best_attempt_idx",
		"CREATE INDEX contest_problem_results_last_attempt_idx",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("contest projection schema missing %q", want)
		}
	}
}

func TestJudgeResultQueriesExposeAttemptsCasesAndProjection(t *testing.T) {
	for name, query := range map[string]string{
		"CreateJudgeAttempt":                  createJudgeAttempt,
		"GetJudgeAttemptByID":                 getJudgeAttemptByID,
		"GetLatestJudgeAttemptBySubmissionID": getLatestJudgeAttemptBySubmissionID,
		"GetLatestJudgeAttemptByRunID":        getLatestJudgeAttemptByRunID,
		"ListJudgeAttemptsBySubmissionID":     listJudgeAttemptsBySubmissionID,
		"MarkJudgeAttemptFinished":            markJudgeAttemptFinished,
		"CreateJudgeCaseResult":               createJudgeCaseResult,
		"ListJudgeCaseResultsByAttemptID":     listJudgeCaseResultsByAttemptID,
		"UpsertSubmissionResult":              upsertSubmissionResult,
		"GetSubmissionResultBySubmissionID":   getSubmissionResultBySubmissionID,
	} {
		if !strings.Contains(query, "judge_") && !strings.Contains(query, "submission_results") {
			t.Fatalf("%s does not target judge result tables:\n%s", name, query)
		}
	}
	if !strings.Contains(upsertSubmissionResult, "ON CONFLICT (submission_id) DO UPDATE") {
		t.Fatalf("UpsertSubmissionResult should maintain current projection:\n%s", upsertSubmissionResult)
	}
}

func TestProblemCheckSchemaAndQueriesExposeRunsAndFindings(t *testing.T) {
	schema := readInitialSchema(t)
	for _, want := range []string{
		"CREATE TABLE problem_check_runs",
		"CREATE TABLE problem_check_findings",
		"problem_id bigint NOT NULL REFERENCES problems(id) ON DELETE CASCADE",
		"run_id bigint NOT NULL REFERENCES problem_check_runs(id) ON DELETE CASCADE",
		"CREATE INDEX problem_check_runs_problem_created_idx",
		"CREATE INDEX problem_check_findings_run_severity_idx",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("problem check schema missing %q", want)
		}
	}

	for name, query := range map[string]string{
		"CreateProblemCheckRun":           createProblemCheckRun,
		"GetProblemCheckRunByID":          getProblemCheckRunByID,
		"ListProblemCheckRunsByProblemID": listProblemCheckRunsByProblemID,
		"CompleteProblemCheckRun":         completeProblemCheckRun,
		"FailProblemCheckRun":             failProblemCheckRun,
		"CreateProblemCheckFinding":       createProblemCheckFinding,
		"GetProblemCheckFindingByID":      getProblemCheckFindingByID,
		"ListProblemCheckFindingsByRunID": listProblemCheckFindingsByRunID,
	} {
		if !strings.Contains(query, "problem_check_") {
			t.Fatalf("%s does not target problem check tables:\n%s", name, query)
		}
	}
	if !strings.Contains(completeProblemCheckRun, "AND status IN ('queued', 'running')") {
		t.Fatalf("CompleteProblemCheckRun should guard mutable statuses:\n%s", completeProblemCheckRun)
	}
	if !strings.Contains(failProblemCheckRun, "AND status IN ('queued', 'running')") {
		t.Fatalf("FailProblemCheckRun should guard mutable statuses:\n%s", failProblemCheckRun)
	}
}

func TestRejudgeBatchSchemaAndQueriesExposeProgressLifecycle(t *testing.T) {
	schema := readInitialSchema(t)
	for _, want := range []string{
		"CREATE TABLE rejudge_batches",
		"rejudge_batch_id bigint REFERENCES rejudge_batches(id) ON DELETE SET NULL",
		"CREATE INDEX rejudge_batches_status_updated_idx",
		"CREATE INDEX rejudge_batches_problem_created_idx",
		"CREATE INDEX judge_attempts_rejudge_batch_id_idx",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("rejudge schema missing %q", want)
		}
	}

	for name, query := range map[string]string{
		"CreateRejudgeBatch":              createRejudgeBatch,
		"GetRejudgeBatchByID":             getRejudgeBatchByID,
		"ListRejudgeBatches":              listRejudgeBatches,
		"UpdateRejudgeBatchProgress":      updateRejudgeBatchProgress,
		"CompleteRejudgeBatch":            completeRejudgeBatch,
		"FailRejudgeBatch":                failRejudgeBatch,
		"CancelRejudgeBatch":              cancelRejudgeBatch,
		"ListJudgeAttemptsByRejudgeBatch": listJudgeAttemptsByRejudgeBatch,
	} {
		if !strings.Contains(query, "rejudge_") {
			t.Fatalf("%s does not target rejudge tables:\n%s", name, query)
		}
	}
	if !strings.Contains(createJudgeAttempt, "rejudge_batch_id") {
		t.Fatalf("CreateJudgeAttempt should accept rejudge_batch_id:\n%s", createJudgeAttempt)
	}
	for name, query := range map[string]string{
		"UpdateRejudgeBatchProgress": updateRejudgeBatchProgress,
		"CompleteRejudgeBatch":       completeRejudgeBatch,
		"FailRejudgeBatch":           failRejudgeBatch,
		"CancelRejudgeBatch":         cancelRejudgeBatch,
	} {
		if !strings.Contains(query, "AND status IN ('queued', 'running')") {
			t.Fatalf("%s should guard mutable statuses:\n%s", name, query)
		}
	}
}

func readInitialSchema(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "migrations", "000001_init.up.sql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read initial migration: %v", err)
	}
	return string(content)
}

func tableSchema(t *testing.T, schema, tableName, nextTableName string) string {
	t.Helper()
	start := strings.Index(schema, "CREATE TABLE "+tableName)
	if start < 0 {
		t.Fatalf("schema missing table %s", tableName)
	}
	end := strings.Index(schema[start:], "CREATE TABLE "+nextTableName)
	if end < 0 {
		t.Fatalf("schema missing table %s after %s", nextTableName, tableName)
	}
	return schema[start : start+end]
}
