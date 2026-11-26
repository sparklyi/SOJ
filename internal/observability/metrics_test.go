package observability

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

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

func TestTestcaseCacheMetricsBoundLabels(t *testing.T) {
	metrics := NewMetrics("test")

	metrics.RecordTestcaseCache("hit")
	metrics.RecordTestcaseCache("unexpected_lookup_1")
	metrics.RecordTestcaseCache("unexpected_lookup_2")

	metrics.ObserveTestcaseCachePhase("download", "success", time.Millisecond)
	metrics.ObserveTestcaseCachePhase("unexpected_phase_1", "unexpected_result_1", time.Millisecond)
	metrics.ObserveTestcaseCachePhase("unexpected_phase_2", "unexpected_result_2", time.Millisecond)

	families, err := metrics.registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	familyByName := make(map[string]*dto.MetricFamily)
	for _, family := range families {
		familyByName[family.GetName()] = family
	}

	labelsFor := func(metric *dto.Metric) map[string]string {
		labels := make(map[string]string)
		for _, label := range metric.GetLabel() {
			labels[label.GetName()] = label.GetValue()
		}
		return labels
	}

	cache := familyByName["soj_judge_agent_testcase_cache_total"]
	if cache == nil {
		t.Fatal("testcase cache counter family not found")
	}

	if got := len(cache.GetMetric()); got != 2 {
		t.Fatalf("expected exactly 2 testcase cache series, got %d", got)
	}

	expectedCache := []struct {
		labels map[string]string
		value  float64
	}{
		{
			labels: map[string]string{
				"result":  "hit",
				"service": "test",
			},
			value: 1,
		},
		{
			labels: map[string]string{
				"result":  "error",
				"service": "test",
			},
			value: 2,
		},
	}

	for _, expected := range expectedCache {
		found := false

		for _, metric := range cache.GetMetric() {
			if reflect.DeepEqual(labelsFor(metric), expected.labels) {
				found = true

				if got := metric.GetCounter().GetValue(); got != expected.value {
					t.Errorf(
						"counter value for labels %v = %v, want %v",
						expected.labels,
						got,
						expected.value,
					)
				}
				break
			}
		}

		if !found {
			t.Errorf("missing counter series with exact labels %v", expected.labels)
		}
	}

	phase := familyByName["soj_judge_agent_testcase_cache_phase_duration_seconds"]
	if phase == nil {
		t.Fatal("testcase cache phase histogram family not found")
	}

	if got := len(phase.GetMetric()); got != 2 {
		t.Fatalf("expected exactly 2 testcase cache phase series, got %d", got)
	}

	expectedPhase := []struct {
		labels      map[string]string
		sampleCount uint64
	}{
		{
			labels: map[string]string{
				"phase":   "download",
				"result":  "success",
				"service": "test",
			},
			sampleCount: 1,
		},
		{
			labels: map[string]string{
				"phase":   "unknown",
				"result":  "error",
				"service": "test",
			},
			sampleCount: 2,
		},
	}

	for _, expected := range expectedPhase {
		found := false

		for _, metric := range phase.GetMetric() {
			if reflect.DeepEqual(labelsFor(metric), expected.labels) {
				found = true

				if got := metric.GetHistogram().GetSampleCount(); got != expected.sampleCount {
					t.Errorf(
						"histogram sample count for labels %v = %d, want %d",
						expected.labels,
						got,
						expected.sampleCount,
					)
				}
				break
			}
		}

		if !found {
			t.Errorf("missing histogram series with exact labels %v", expected.labels)
		}
	}
}

func TestQueueStatsKeepRunRequestSeparate(t *testing.T) {
	metrics := NewMetrics("test")
	metrics.ObserveQueueStats("request", 7, 3, 45*time.Second)
	metrics.ObserveQueueStats("run_request", 11, 5, 90*time.Second)

	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()

	for _, want := range []string{
		`soj_queue_depth{queue="request",service="test"} 7`,
		`soj_queue_depth{queue="run_request",service="test"} 11`,
		`soj_queue_pending_messages{queue="run_request",service="test"} 5`,
		`soj_queue_oldest_pending_age_seconds{queue="run_request",service="test"} 90`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %s:\n%s", want, body)
		}
	}
}
