package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry *prometheus.Registry

	httpRequests           *prometheus.CounterVec
	httpRequestDuration    *prometheus.HistogramVec
	judgeDispatches        *prometheus.CounterVec
	judgeTasks             *prometheus.CounterVec
	judgeTaskDuration      *prometheus.HistogramVec
	resultConsumer         *prometheus.CounterVec
	resultConsumerTime     *prometheus.HistogramVec
	queueDepth             *prometheus.GaugeVec
	queuePending           *prometheus.GaugeVec
	queueOldestPending     *prometheus.GaugeVec
	judgeAgentSlotsUsed    *prometheus.GaugeVec
	judgeAgentSlotsCap     *prometheus.GaugeVec
	sandboxPhaseDuration   *prometheus.HistogramVec
	sandboxBackendErrors   *prometheus.CounterVec
	sandboxCleanupFails    *prometheus.CounterVec
	sandboxCleanupTimeouts *prometheus.CounterVec
	readinessChecks        *prometheus.CounterVec
	readinessDuration      *prometheus.HistogramVec
	taskRecovery           *prometheus.CounterVec
	reconcilerActions      *prometheus.CounterVec
	rejudgeBatches         *prometheus.CounterVec
}

func NewMetrics(service string) *Metrics {
	if service == "" {
		service = "soj"
	}
	labels := prometheus.Labels{"service": service}
	registry := prometheus.NewRegistry()
	metrics := &Metrics{
		registry: registry,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "soj",
			Name:        "http_requests_total",
			Help:        "Total number of HTTP requests handled by SOJ.",
			ConstLabels: labels,
		}, []string{"method", "route", "status"}),
		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   "soj",
			Name:        "http_request_duration_seconds",
			Help:        "HTTP request duration in seconds.",
			ConstLabels: labels,
			Buckets:     []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"method", "route", "status"}),
		judgeDispatches: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "soj",
			Subsystem:   "worker",
			Name:        "judge_task_dispatch_total",
			Help:        "Total number of judge task dispatch attempts.",
			ConstLabels: labels,
		}, []string{"result"}),
		judgeTasks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "soj",
			Subsystem:   "worker",
			Name:        "judge_tasks_total",
			Help:        "Total number of judge tasks processed by workers.",
			ConstLabels: labels,
		}, []string{"result"}),
		judgeTaskDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   "soj",
			Subsystem:   "worker",
			Name:        "judge_task_duration_seconds",
			Help:        "Judge task processing duration in seconds.",
			ConstLabels: labels,
			Buckets:     []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"result"}),
		resultConsumer: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "soj",
			Subsystem:   "worker",
			Name:        "result_consumer_messages_total",
			Help:        "Total number of result-consumer messages processed by outcome.",
			ConstLabels: labels,
		}, []string{"queue", "result"}),
		resultConsumerTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   "soj",
			Subsystem:   "worker",
			Name:        "result_consumer_duration_seconds",
			Help:        "Result-consumer message processing duration in seconds.",
			ConstLabels: labels,
			Buckets:     []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"queue", "result"}),
		queueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   "soj",
			Name:        "queue_depth",
			Help:        "Current Redis-backed logical queue stream length.",
			ConstLabels: labels,
		}, []string{"queue"}),
		queuePending: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   "soj",
			Name:        "queue_pending_messages",
			Help:        "Current Redis-backed logical queue pending message count.",
			ConstLabels: labels,
		}, []string{"queue"}),
		queueOldestPending: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   "soj",
			Name:        "queue_oldest_pending_age_seconds",
			Help:        "Age of the oldest pending message in a Redis-backed logical queue.",
			ConstLabels: labels,
		}, []string{"queue"}),
		judgeAgentSlotsUsed: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   "soj",
			Subsystem:   "judge_agent",
			Name:        "slots_used",
			Help:        "Currently occupied judge-agent sandbox slots.",
			ConstLabels: labels,
		}, []string{"scope", "language"}),
		judgeAgentSlotsCap: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   "soj",
			Subsystem:   "judge_agent",
			Name:        "slots_capacity",
			Help:        "Configured judge-agent sandbox slot capacity.",
			ConstLabels: labels,
		}, []string{"scope", "language"}),
		sandboxPhaseDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   "soj",
			Subsystem:   "sandbox",
			Name:        "phase_duration_seconds",
			Help:        "Sandbox backend phase duration in seconds.",
			ConstLabels: labels,
			Buckets:     []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"backend", "phase", "result"}),
		sandboxBackendErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "soj",
			Subsystem:   "sandbox",
			Name:        "backend_errors_total",
			Help:        "Sandbox backend errors by backend, phase, and class.",
			ConstLabels: labels,
		}, []string{"backend", "phase", "class"}),
		sandboxCleanupFails: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "soj",
			Subsystem:   "sandbox",
			Name:        "cleanup_failures_total",
			Help:        "Sandbox cleanup failures by resource.",
			ConstLabels: labels,
		}, []string{"backend", "resource"}),
		sandboxCleanupTimeouts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "soj",
			Subsystem:   "sandbox",
			Name:        "cleanup_timeouts_total",
			Help:        "Sandbox cleanup timeouts by resource.",
			ConstLabels: labels,
		}, []string{"backend", "resource"}),
		readinessChecks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "soj",
			Name:        "readiness_checks_total",
			Help:        "Total number of readiness dependency checks.",
			ConstLabels: labels,
		}, []string{"dependency", "result"}),
		readinessDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   "soj",
			Name:        "readiness_check_duration_seconds",
			Help:        "Readiness dependency check duration in seconds.",
			ConstLabels: labels,
			Buckets:     []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		}, []string{"dependency", "result"}),
		taskRecovery: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "soj",
			Subsystem:   "worker",
			Name:        "judge_task_recovery_total",
			Help:        "Judge task recovery operations by action and result.",
			ConstLabels: labels,
		}, []string{"action", "result"}),
		reconcilerActions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "soj",
			Subsystem:   "worker",
			Name:        "reconciliation_total",
			Help:        "Worker reconciliation actions by action and result.",
			ConstLabels: labels,
		}, []string{"action", "result"}),
		rejudgeBatches: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "soj",
			Subsystem:   "rejudge",
			Name:        "batches_total",
			Help:        "Rejudge batch operations by action, target type, and result.",
			ConstLabels: labels,
		}, []string{"action", "target", "result"}),
	}

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		metrics.httpRequests,
		metrics.httpRequestDuration,
		metrics.judgeDispatches,
		metrics.judgeTasks,
		metrics.judgeTaskDuration,
		metrics.resultConsumer,
		metrics.resultConsumerTime,
		metrics.queueDepth,
		metrics.queuePending,
		metrics.queueOldestPending,
		metrics.judgeAgentSlotsUsed,
		metrics.judgeAgentSlotsCap,
		metrics.sandboxPhaseDuration,
		metrics.sandboxBackendErrors,
		metrics.sandboxCleanupFails,
		metrics.sandboxCleanupTimeouts,
		metrics.readinessChecks,
		metrics.readinessDuration,
		metrics.taskRecovery,
		metrics.reconcilerActions,
		metrics.rejudgeBatches,
	)
	return metrics
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	code := strconv.Itoa(status)
	m.httpRequests.WithLabelValues(method, route, code).Inc()
	m.httpRequestDuration.WithLabelValues(method, route, code).Observe(duration.Seconds())
}

func (m *Metrics) RecordJudgeTaskDispatch(result string) {
	m.judgeDispatches.WithLabelValues(result).Inc()
}

func (m *Metrics) RecordJudgeTaskProcess(result string, duration time.Duration) {
	m.judgeTasks.WithLabelValues(result).Inc()
	m.judgeTaskDuration.WithLabelValues(result).Observe(duration.Seconds())
}

func (m *Metrics) RecordResultConsumerProcess(queueName, result string, duration time.Duration) {
	queueName = logicalQueueLabel(queueName)
	result = boundedResultLabel(result)
	m.resultConsumer.WithLabelValues(queueName, result).Inc()
	m.resultConsumerTime.WithLabelValues(queueName, result).Observe(duration.Seconds())
}

func (m *Metrics) ObserveQueueStats(queueName string, depth, pending int64, oldestPendingAge time.Duration) {
	queueName = logicalQueueLabel(queueName)
	m.queueDepth.WithLabelValues(queueName).Set(float64(depth))
	m.queuePending.WithLabelValues(queueName).Set(float64(pending))
	m.queueOldestPending.WithLabelValues(queueName).Set(oldestPendingAge.Seconds())
}

func (m *Metrics) ObserveJudgeAgentSlots(scope, language string, used, capacity int) {
	m.judgeAgentSlotsUsed.WithLabelValues(scope, language).Set(float64(used))
	m.judgeAgentSlotsCap.WithLabelValues(scope, language).Set(float64(capacity))
}

func (m *Metrics) ObserveSandboxPhase(backend, phase, result string, duration time.Duration) {
	m.sandboxPhaseDuration.WithLabelValues(backend, phase, result).Observe(duration.Seconds())
}

func (m *Metrics) RecordSandboxBackendError(backend, phase, class string) {
	m.sandboxBackendErrors.WithLabelValues(backend, phase, class).Inc()
}

func (m *Metrics) RecordSandboxCleanupFailure(backend, resource string) {
	m.sandboxCleanupFails.WithLabelValues(backend, resource).Inc()
}

func (m *Metrics) RecordSandboxCleanupTimeout(backend, resource string) {
	m.sandboxCleanupTimeouts.WithLabelValues(backend, resource).Inc()
}

func (m *Metrics) RecordReadinessCheck(dependency, result string, duration time.Duration) {
	m.readinessChecks.WithLabelValues(dependency, result).Inc()
	m.readinessDuration.WithLabelValues(dependency, result).Observe(duration.Seconds())
}

func (m *Metrics) RecordJudgeTaskRecovery(action, result string) {
	m.taskRecovery.WithLabelValues(action, result).Inc()
}

func (m *Metrics) RecordRejudgeBatch(action, target, result string) {
	m.rejudgeBatches.WithLabelValues(action, target, boundedResultLabel(result)).Inc()
}

func (m *Metrics) RecordReconcilerAction(action, result string, count int) {
	if count <= 0 {
		count = 1
	}
	m.reconcilerActions.WithLabelValues(action, result).Add(float64(count))
}

func logicalQueueLabel(queueName string) string {
	switch queueName {
	case "request", "result":
		return queueName
	default:
		return "unknown"
	}
}

func boundedResultLabel(result string) string {
	switch result {
	case "success", "error":
		return result
	default:
		return "error"
	}
}
