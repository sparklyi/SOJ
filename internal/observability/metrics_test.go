package observability

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestJudgeRequestPayloadHistogram gathers the judge request payload
// histogram in-process from a fresh registry: RecordJudgeRequestPayloadSize
// clamps negatives to zero, and the observations land in the expected
// buckets (0 and 512 both count in le="512", 1024 in le="1024").
func TestJudgeRequestPayloadHistogram(t *testing.T) {
	metrics := NewMetrics("test")
	metrics.RecordJudgeRequestPayloadSize(-1) // clamped to 0
	metrics.RecordJudgeRequestPayloadSize(512)
	metrics.RecordJudgeRequestPayloadSize(1024)

	expected := `# HELP soj_worker_judge_request_payload_size_bytes Judge request payload size in bytes.
# TYPE soj_worker_judge_request_payload_size_bytes histogram
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="512"} 2
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="1024"} 3
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="4096"} 3
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="16384"} 3
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="65536"} 3
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="262144"} 3
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="1048576"} 3
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="4194304"} 3
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="+Inf"} 3
soj_worker_judge_request_payload_size_bytes_sum{service="test"} 1536
soj_worker_judge_request_payload_size_bytes_count{service="test"} 3
`
	if err := testutil.GatherAndCompare(metrics.registry, strings.NewReader(expected), "soj_worker_judge_request_payload_size_bytes"); err != nil {
		t.Fatalf("histogram mismatch: %v", err)
	}
}

// A negative payload size must be observed as 0, not dropped or recorded as
// a negative value, so the sum stays meaningful.
func TestJudgeRequestPayloadHistogramClampsNegativeToZero(t *testing.T) {
	metrics := NewMetrics("test")
	metrics.RecordJudgeRequestPayloadSize(-100)

	expected := `# HELP soj_worker_judge_request_payload_size_bytes Judge request payload size in bytes.
# TYPE soj_worker_judge_request_payload_size_bytes histogram
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="512"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="1024"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="4096"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="16384"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="65536"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="262144"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="1048576"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="4194304"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="+Inf"} 1
soj_worker_judge_request_payload_size_bytes_sum{service="test"} 0
soj_worker_judge_request_payload_size_bytes_count{service="test"} 1
`
	if err := testutil.GatherAndCompare(metrics.registry, strings.NewReader(expected), "soj_worker_judge_request_payload_size_bytes"); err != nil {
		t.Fatalf("histogram mismatch: %v", err)
	}
}

// Bucket boundaries are inclusive (le="..."), so 513 lands one bucket above
// 512, 4096 exactly in le="4096", and a value beyond the top finite bucket
// only in le="+Inf".
func TestJudgeRequestPayloadHistogramBucketBoundaries(t *testing.T) {
	metrics := NewMetrics("test")
	metrics.RecordJudgeRequestPayloadSize(513)
	metrics.RecordJudgeRequestPayloadSize(4096)
	metrics.RecordJudgeRequestPayloadSize(5_000_000)

	expected := `# HELP soj_worker_judge_request_payload_size_bytes Judge request payload size in bytes.
# TYPE soj_worker_judge_request_payload_size_bytes histogram
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="512"} 0
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="1024"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="4096"} 2
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="16384"} 2
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="65536"} 2
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="262144"} 2
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="1048576"} 2
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="4194304"} 2
soj_worker_judge_request_payload_size_bytes_bucket{service="test",le="+Inf"} 3
soj_worker_judge_request_payload_size_bytes_sum{service="test"} 5004609
soj_worker_judge_request_payload_size_bytes_count{service="test"} 3
`
	if err := testutil.GatherAndCompare(metrics.registry, strings.NewReader(expected), "soj_worker_judge_request_payload_size_bytes"); err != nil {
		t.Fatalf("histogram mismatch: %v", err)
	}
}

// Each NewMetrics gets a fresh registry, so observations from one service
// never leak into another's histogram.
func TestJudgeRequestPayloadHistogramIsPerRegistry(t *testing.T) {
	a := NewMetrics("svc-a")
	b := NewMetrics("svc-b")
	a.RecordJudgeRequestPayloadSize(100)

	bOnly := `# HELP soj_worker_judge_request_payload_size_bytes Judge request payload size in bytes.
# TYPE soj_worker_judge_request_payload_size_bytes histogram
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-b",le="512"} 0
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-b",le="1024"} 0
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-b",le="4096"} 0
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-b",le="16384"} 0
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-b",le="65536"} 0
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-b",le="262144"} 0
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-b",le="1.048576e+06"} 0
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-b",le="4.194304e+06"} 0
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-b",le="+Inf"} 0
soj_worker_judge_request_payload_size_bytes_sum{service="svc-b"} 0
soj_worker_judge_request_payload_size_bytes_count{service="svc-b"} 0
`
	if err := testutil.GatherAndCompare(b.registry, strings.NewReader(bOnly), "soj_worker_judge_request_payload_size_bytes"); err != nil {
		t.Fatalf("svc-b histogram should be empty: %v", err)
	}

	aExpected := `# HELP soj_worker_judge_request_payload_size_bytes Judge request payload size in bytes.
# TYPE soj_worker_judge_request_payload_size_bytes histogram
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-a",le="512"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-a",le="1024"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-a",le="4096"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-a",le="16384"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-a",le="65536"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-a",le="262144"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-a",le="1.048576e+06"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-a",le="4.194304e+06"} 1
soj_worker_judge_request_payload_size_bytes_bucket{service="svc-a",le="+Inf"} 1
soj_worker_judge_request_payload_size_bytes_sum{service="svc-a"} 100
soj_worker_judge_request_payload_size_bytes_count{service="svc-a"} 1
`
	if err := testutil.GatherAndCompare(a.registry, strings.NewReader(aExpected), "soj_worker_judge_request_payload_size_bytes"); err != nil {
		t.Fatalf("svc-a histogram mismatch: %v", err)
	}
}

func TestMetricsExposeJudgeAgentAndSandboxSignals(t *testing.T) {
	metrics := NewMetrics("test")
	metrics.ObserveJudgeAgentSlots("global", "", 1, 2)
	metrics.ObserveJudgeAgentSlots("language", "go", 1, 1)
	metrics.ObserveQueueStats("request", 7, 3, 45*time.Second)
	metrics.RecordResultConsumerProcess("result", "success", 25*time.Millisecond)
	metrics.RecordResultConsumerProcess("result", "error", 10*time.Millisecond)
	metrics.ObserveSandboxPhase("docker", "run", "accepted", 120*time.Millisecond)
	metrics.RecordSandboxBackendError("docker", "probe", "runtime_unavailable")
	metrics.RecordSandboxCleanupFailure("docker", "container")
	metrics.RecordSandboxCleanupTimeout("docker", "workspace")
	metrics.RecordReadinessCheck("redis.request_stream", "success", 10*time.Millisecond)
	metrics.RecordJudgeTaskRecovery("recover_dead_task", "success")
	metrics.RecordReconcilerAction("reset_stale_tasks", "success", 2)
	metrics.RecordRejudgeBatch("create", "problem", "success")
	metrics.RecordJudgeRequestPayloadSize(1024)
	metrics.RecordTestcaseCache("miss")
	metrics.RecordTestcaseCache("hit")
	metrics.ObserveTestcaseCachePhase("download", "success", 20*time.Millisecond)
	metrics.ObserveTestcaseCachePhase("unpack", "error", 30*time.Millisecond)
	metrics.ObserveTestcaseCacheBytes(4096)
	metrics.RecordTestcaseCacheEviction()

	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"soj_judge_agent_slots_used",
		"soj_judge_agent_slots_capacity",
		`soj_queue_depth{queue="request",service="test"} 7`,
		`soj_queue_pending_messages{queue="request",service="test"} 3`,
		`soj_queue_oldest_pending_age_seconds{queue="request",service="test"} 45`,
		`soj_worker_result_consumer_messages_total{queue="result",result="success",service="test"} 1`,
		`soj_worker_result_consumer_messages_total{queue="result",result="error",service="test"} 1`,
		"soj_worker_result_consumer_duration_seconds",
		"soj_sandbox_phase_duration_seconds",
		"soj_sandbox_backend_errors_total",
		`soj_sandbox_cleanup_failures_total{backend="docker",resource="container",service="test"} 1`,
		`soj_sandbox_cleanup_timeouts_total{backend="docker",resource="workspace",service="test"} 1`,
		"soj_readiness_checks_total",
		"soj_readiness_check_duration_seconds",
		"soj_worker_judge_task_recovery_total",
		"soj_worker_reconciliation_total",
		`soj_rejudge_batches_total{action="create",result="success",service="test",target="problem"} 1`,
		"soj_worker_judge_request_payload_size_bytes",
		"soj_judge_agent_testcase_cache_total",
		"soj_judge_agent_testcase_cache_phase_duration_seconds",
		`soj_judge_agent_testcase_cache_bytes{service="test"} 4096`,
		"soj_judge_agent_testcase_cache_evictions_total",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %s:\n%s", want, body)
		}
	}
}
