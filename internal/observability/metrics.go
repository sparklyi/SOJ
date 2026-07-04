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

	httpRequests        *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	judgeDispatches     *prometheus.CounterVec
	judgeTasks          *prometheus.CounterVec
	judgeTaskDuration   *prometheus.HistogramVec
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
	}

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		metrics.httpRequests,
		metrics.httpRequestDuration,
		metrics.judgeDispatches,
		metrics.judgeTasks,
		metrics.judgeTaskDuration,
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
