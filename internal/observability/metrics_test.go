package observability

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsExposeJudgeAgentAndSandboxSignals(t *testing.T) {
	metrics := NewMetrics("test")
	metrics.ObserveJudgeAgentSlots("global", "", 1, 2)
	metrics.ObserveJudgeAgentSlots("language", "go", 1, 1)
	metrics.ObserveSandboxPhase("docker", "run", "accepted", 120*time.Millisecond)
	metrics.RecordSandboxBackendError("docker", "probe", "runtime_unavailable")
	metrics.RecordSandboxCleanupFailure("docker")
	metrics.RecordReadinessCheck("redis.request_stream", "success", 10*time.Millisecond)
	metrics.RecordJudgeTaskRecovery("recover_dead_task", "success")
	metrics.RecordReconcilerAction("reset_stale_tasks", "success", 2)

	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"soj_judge_agent_slots_used",
		"soj_judge_agent_slots_capacity",
		"soj_sandbox_phase_duration_seconds",
		"soj_sandbox_backend_errors_total",
		"soj_sandbox_cleanup_failures_total",
		"soj_readiness_checks_total",
		"soj_readiness_check_duration_seconds",
		"soj_worker_judge_task_recovery_total",
		"soj_worker_reconciliation_total",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %s:\n%s", want, body)
		}
	}
}
