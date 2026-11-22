package observability

import (
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// payloadHistogram is the judge request payload histogram collected from a
// registry with Gather(), with the bucket upper bounds (le="...", +Inf last)
// and their cumulative counts kept as plain values.
type payloadHistogram struct {
	labels map[string]string
	le     []float64
	counts []uint64
	sum    float64
	count  uint64
}

// gatherPayloadHistogram gathers the metrics from the registry and returns the
// single `soj_worker_judge_request_payload_size_bytes` histogram, read purely
// through the returned values so no extra module is needed in go.mod.
func gatherPayloadHistogram(t *testing.T, m *Metrics) payloadHistogram {
	t.Helper()
	mfs, err := m.registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "soj_worker_judge_request_payload_size_bytes" {
			continue
		}
		if len(mf.GetMetric()) != 1 {
			t.Fatalf("expected exactly one metric for the payload histogram, got %d", len(mf.GetMetric()))
		}
		metric := mf.GetMetric()[0]
		h := metric.GetHistogram()
		out := payloadHistogram{
			labels: map[string]string{},
			sum:    h.GetSampleSum(),
			count:  h.GetSampleCount(),
		}
		for _, lp := range metric.GetLabel() {
			out.labels[lp.GetName()] = lp.GetValue()
		}
		for _, b := range h.GetBucket() {
			out.le = append(out.le, b.GetUpperBound())
			out.counts = append(out.counts, b.GetCumulativeCount())
		}
		return out
	}
	t.Fatal("soj_worker_judge_request_payload_size_bytes not found in gathered metrics")
	return payloadHistogram{}
}

func assertPayloadHistogram(t *testing.T, h payloadHistogram, wantLabels map[string]string, wantCounts []uint64, wantSum float64, wantCount uint64) {
	t.Helper()
	if len(wantCounts) != len(h.counts) {
		t.Fatalf("bucket count mismatch: got %d buckets %v, want %d", len(h.counts), h.le, len(wantCounts))
	}
	for k, v := range wantLabels {
		if h.labels[k] != v {
			t.Fatalf("label %q = %q, want %q (labels: %v)", k, h.labels[k], v, h.labels)
		}
	}
	for i, want := range wantCounts {
		if h.counts[i] != want {
			t.Fatalf("bucket le=%v cumulative count = %d, want %d (all: %v)", h.le[i], h.counts[i], want, h.counts)
		}
	}
	if h.count != wantCount {
		t.Fatalf("sample count = %d, want %d", h.count, wantCount)
	}
	if math.Abs(h.sum-wantSum) > 1e-9 {
		t.Fatalf("sample sum = %v, want %v", h.sum, wantSum)
	}
}

// All finite buckets, in order. Gather() omits the implicit le="+Inf" bucket; its cumulative count equals the sample count.
var allBucketBounds = []float64{512, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304}

// TestJudgeRequestPayloadHistogram gathers the judge request payload
// histogram via registry.Gather(): RecordJudgeRequestPayloadSize clamps
// negatives to zero, and the observations land in the expected cumulative
// buckets (0 and 512 both count in le="512", 1024 in le="1024").
func TestJudgeRequestPayloadHistogram(t *testing.T) {
	metrics := NewMetrics("test")
	metrics.RecordJudgeRequestPayloadSize(-1) // clamped to 0
	metrics.RecordJudgeRequestPayloadSize(512)
	metrics.RecordJudgeRequestPayloadSize(1024)

	h := gatherPayloadHistogram(t, metrics)
	assertPayloadHistogram(t, h,
		map[string]string{"service": "test"},
		[]uint64{2, 3, 3, 3, 3, 3, 3, 3},
		1536, 3)
}

// A negative payload size must be observed as 0, not dropped or recorded as
// a negative value, so the sum stays meaningful.
func TestJudgeRequestPayloadHistogramClampsNegativeToZero(t *testing.T) {
	metrics := NewMetrics("test")
	metrics.RecordJudgeRequestPayloadSize(-100)

	h := gatherPayloadHistogram(t, metrics)
	assertPayloadHistogram(t, h,
		map[string]string{"service": "test"},
		[]uint64{1, 1, 1, 1, 1, 1, 1, 1},
		0, 1)
}

// Bucket boundaries are inclusive (le="..."), so 513 lands one bucket above
// 512, 4096 exactly in le="4096", and a value beyond the top finite bucket
// only in le="+Inf".
func TestJudgeRequestPayloadHistogramBucketBoundaries(t *testing.T) {
	metrics := NewMetrics("test")
	metrics.RecordJudgeRequestPayloadSize(513)
	metrics.RecordJudgeRequestPayloadSize(4096)
	metrics.RecordJudgeRequestPayloadSize(5_000_000)

	h := gatherPayloadHistogram(t, metrics)
	assertPayloadHistogram(t, h,
		map[string]string{"service": "test"},
		[]uint64{0, 1, 2, 2, 2, 2, 2, 2},
		5004609, 3)
}

// Each NewMetrics gets a fresh registry, so observations from one service
// never leak into another's histogram.
func TestJudgeRequestPayloadHistogramIsPerRegistry(t *testing.T) {
	a := NewMetrics("svc-a")
	b := NewMetrics("svc-b")
	a.RecordJudgeRequestPayloadSize(100)

	hb := gatherPayloadHistogram(t, b)
	assertPayloadHistogram(t, hb,
		map[string]string{"service": "svc-b"},
		[]uint64{0, 0, 0, 0, 0, 0, 0, 0},
		0, 0)

	ha := gatherPayloadHistogram(t, a)
	assertPayloadHistogram(t, ha,
		map[string]string{"service": "svc-a"},
		[]uint64{1, 1, 1, 1, 1, 1, 1, 1},
		100, 1)
}

// The gathered bucket upper bounds match the configured finite buckets, so
// the assertions above cannot silently pass on a different bucket layout.
func TestJudgeRequestPayloadHistogramBucketLayout(t *testing.T) {
	metrics := NewMetrics("test")
	h := gatherPayloadHistogram(t, metrics)
	if len(h.le) != len(allBucketBounds) {
		t.Fatalf("got buckets %v, want %v", h.le, allBucketBounds)
	}
	for i, want := range allBucketBounds {
		if h.le[i] != want {
			t.Fatalf("bucket %d upper bound = %v, want %v (all: %v)", i, h.le[i], want, h.le)
		}
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
