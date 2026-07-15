# SOJ Observability Trial Loop

This guide is the dashboard-as-document artifact for the SOJ async judge trial deployment. It gives operators PromQL panels, alert interpretation, and the path from an alert to an OpenTelemetry trace or persisted judge attempt.

The local Compose stack includes Prometheus and loads `deploy/prometheus-rules/soj-alerts.yml`. It does not add or require Grafana, Alertmanager, Jaeger, Tempo, or an OpenTelemetry collector. Use the queries below in the Prometheus UI or copy them into an external dashboard if your environment already has one.

Keep `/metrics` and any tracing backend on a private network or behind ingress protection.

## Signal Map

| Area | Primary signals | Use when |
| --- | --- | --- |
| API health | `soj_http_requests_total`, `soj_http_request_duration_seconds` | Requests fail or get slow. |
| Worker queue flow | `soj_queue_depth`, `soj_queue_pending_messages`, `soj_queue_oldest_pending_age_seconds`, `soj_worker_judge_task_dispatch_total`, `soj_worker_result_consumer_messages_total` | Submissions remain queued or result events do not land. |
| Judge-agent capacity | `soj_judge_agent_slots_used`, `soj_judge_agent_slots_capacity` | Judge throughput is low or request backlog grows. |
| Sandbox latency | `soj_sandbox_phase_duration_seconds` | Compile/run/check phases get slow. |
| Verdict/error distribution | `soj_worker_judge_tasks_total`, `soj_sandbox_backend_errors_total`, `soj_sandbox_cleanup_failures_total` | Verdicts shift, tasks die, or backend failures increase. |
| Readiness | `soj_readiness_checks_total`, `soj_readiness_check_duration_seconds` | `/readyz` fails or a dependency flaps. |
| Recovery | `soj_worker_judge_task_recovery_total`, `soj_worker_reconciliation_total` | Operators recover dead tasks or reconciliation starts changing state. |

Prometheus adds the scrape `job` label (`soj-api`, `soj-worker`, `soj-judge-agent`). SOJ also emits a constant `service` label from each process. Prefer `job` for deployment dashboards and `service` when you need the process name emitted by SOJ itself.

## Dashboard Panels

### API Health

Request rate by route and status:

```promql
sum by (job, method, route, status) (
  rate(soj_http_requests_total[5m])
)
```

Use this to separate normal traffic from 4xx/5xx spikes. Route labels are normalized, so this panel should stay low-cardinality.

HTTP p95 latency:

```promql
histogram_quantile(
  0.95,
  sum by (job, route, le) (
    rate(soj_http_request_duration_seconds_bucket[5m])
  )
)
```

Start with the routes above the `SOJHTTPLatencyHigh` alert threshold, then compare API readiness and downstream worker queue panels before assuming the API handler itself is the root cause.

5xx rate:

```promql
sum by (job, route) (
  rate(soj_http_requests_total{status=~"5.."}[5m])
)
```

Pivot from a 5xx spike to logs with `request_id`. If tracing is enabled, use the request time window and route to find the matching API server span, then follow child spans into worker and judge-agent processing.

### Worker Queue Flow

Logical queue depth:

```promql
soj_queue_depth{queue=~"request|result"}
```

`request` backlog means the worker or judge-agent path is not draining request events quickly enough. `result` backlog means result events are not being persisted quickly enough by the worker result consumer.

Pending messages:

```promql
soj_queue_pending_messages{queue=~"request|result"}
```

Pending messages indicate messages delivered to a consumer group but not acknowledged. A rising pending count with stable stream depth usually points to a stalled consumer or repeated processing errors.

Oldest pending age:

```promql
max by (job, queue) (
  soj_queue_oldest_pending_age_seconds{queue=~"request|result"}
)
```

Use this as the stuck-work panel. The `SOJQueueOldestPendingAgeHigh` alert fires when the oldest pending message stays older than the trial threshold.

Dispatch rate by result:

```promql
sum by (job, result) (
  rate(soj_worker_judge_task_dispatch_total[5m])
)
```

Expected healthy flow is mostly `success`. Any sustained `error` result should be correlated with Redis readiness, object storage readiness, and worker logs.

Result-consumer rate and errors:

```promql
sum by (job, queue, result) (
  rate(soj_worker_result_consumer_messages_total[5m])
)
```

`result="error"` means the worker failed while handling a result event. Check database connectivity, idempotent terminal writes, and event validation before replaying or recovering tasks.

Result-consumer p95 duration:

```promql
histogram_quantile(
  0.95,
  sum by (job, queue, result, le) (
    rate(soj_worker_result_consumer_duration_seconds_bucket[5m])
  )
)
```

Use this when result events exist but final submission state lags. Long durations often mean PostgreSQL writes or projection updates are slow.

### Judge-Agent Capacity

Slot utilization:

```promql
soj_judge_agent_slots_used{scope=~"global|language"}
/
clamp_min(soj_judge_agent_slots_capacity{scope=~"global|language"}, 1)
```

The `scope` and `language` labels show whether the bottleneck is global capacity or a per-language slot cap. A high ratio with growing `request` queue depth means the judge-agent is saturated.

Free slots:

```promql
soj_judge_agent_slots_capacity{scope=~"global|language"}
-
soj_judge_agent_slots_used{scope=~"global|language"}
```

If free slots remain available while queue depth grows, check judge-agent readiness, Redis consumption, and sandbox startup errors rather than increasing slots.

### Sandbox Latency

Sandbox phase p95:

```promql
histogram_quantile(
  0.95,
  sum by (job, backend, phase, result, le) (
    rate(soj_sandbox_phase_duration_seconds_bucket[5m])
  )
)
```

Group by `backend` and `phase` to see whether time is spent in prepare, compile, run, check, or cleanup-related phases. Compare `result` to separate successful slow phases from failed attempts.

Sandbox phase rate:

```promql
sum by (job, backend, phase, result) (
  rate(soj_sandbox_phase_duration_seconds_count[5m])
)
```

Use this with latency to distinguish "slow because there is more traffic" from "slow per attempt."

### Verdict And Error Distribution

Judge task outcomes:

```promql
sum by (job, result) (
  rate(soj_worker_judge_tasks_total[10m])
)
```

This shows processed Redis request outcomes such as `success`, `retry`, `dead`, `skipped`, and `error`. `dead` should be treated as page-level during trial deployment because it means the retry budget was exhausted.

Sandbox backend errors:

```promql
sum by (job, backend, phase, class) (
  rate(soj_sandbox_backend_errors_total[5m])
)
```

Use `backend`, `phase`, and bounded `class` labels to identify infrastructure or runtime failures without exposing source code, object keys, or raw error strings.

Sandbox cleanup failures and timeouts:

```promql
sum by (job, backend, resource) (
  rate(soj_sandbox_cleanup_failures_total[10m])
)
```

```promql
sum by (job, backend, resource) (
  rate(soj_sandbox_cleanup_timeouts_total[10m])
)
```

Use `resource` to distinguish container deletion from workspace removal. Cleanup failures can leave work directories or containers behind. Investigate runner host cleanup before raising capacity.

### Readiness

Readiness failures:

```promql
sum by (job, dependency) (
  increase(soj_readiness_checks_total{result="error"}[5m])
)
```

This backs the `SOJReadinessDependencyFailure` alert. Dependency labels are bounded names, not DSNs, bucket names, or internal addresses.

Readiness p95 latency:

```promql
histogram_quantile(
  0.95,
  sum by (job, dependency, le) (
    rate(soj_readiness_check_duration_seconds_bucket[5m])
  )
)
```

Use this before a dependency fully fails. A rising readiness duration can explain HTTP latency or queue lag even when `/readyz` still returns success.

### Recovery

Recovery operations:

```promql
sum by (job, action, result) (
  increase(soj_worker_judge_task_recovery_total[24h])
)
```

This is an audit panel. A recovery event means an operator or recovery command changed task state. Pair it with notes about the fixed dependency and the affected task IDs.

Reconciliation activity:

```promql
sum by (job, action, result) (
  increase(soj_worker_reconciliation_total[1h])
)
```

`result="error"` maps to the `SOJWorkerReconciliationFailures` alert. Non-error reconciliation activity is still useful context when explaining state changes after outages.

## Alerts

The checked-in alert rules live in `deploy/prometheus-rules/soj-alerts.yml` and are loaded by `deploy/prometheus.yml` for local Prometheus. They are trial thresholds; tune them for production traffic once baseline volume is known.

| Alert | Severity | First action |
| --- | --- | --- |
| `SOJReadinessDependencyFailure` | warning | Check the dependency label, then inspect that process `/readyz` and logs. |
| `SOJHTTP5xxResponses` | warning | Check the route, request logs, and downstream readiness. |
| `SOJHTTPLatencyHigh` | warning | Compare API p95 with readiness latency and queue panels. |
| `SOJJudgeDispatchFailures` | warning | Check worker Redis/object storage readiness and dispatch logs. |
| `SOJResultConsumerFailures` | warning | Check result event validation, PostgreSQL writes, and worker logs. |
| `SOJJudgeTasksDead` | page | Inspect task errors and fix the underlying dependency before recovery. |
| `SOJJudgeTaskRecoveryActivity` | warning | Confirm the recovery was intentional and record the reason. |
| `SOJWorkerReconciliationFailures` | warning | Inspect reconciler logs and database connectivity. |
| `SOJJudgeAgentSlotSaturation` | warning | Compare slot utilization with queue depth before changing capacity. |
| `SOJSandboxBackendErrors` | warning | Check backend, phase, class, runner images, and runtime availability. |
| `SOJSandboxCleanupFailures` | warning | Inspect runner host cleanup and leftover containers/workspaces. |
| `SOJQueueBacklogHigh` | warning | Identify whether request or result queue is not draining. |
| `SOJQueueOldestPendingAgeHigh` | warning | Find the oldest pending consumer message and correlate with logs/traces. |

## Tracing Enablement

Tracing is disabled by default. Generic `OTEL_*` variables alone do not enable SOJ tracing. Set the SOJ gate explicitly:

```bash
SOJ_TRACING_ENABLED=true
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://collector:4318/v1/traces
OTEL_SERVICE_NAME=soj-api
OTEL_RESOURCE_ATTRIBUTES=deployment.environment=trial
```

Repeat the setting for each process that should export spans. If `OTEL_SERVICE_NAME` is omitted, SOJ uses process-specific names such as `soj-api`, `soj-worker`, and `soj-judge-agent`. `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` takes precedence over `OTEL_EXPORTER_OTLP_ENDPOINT`.

SOJ currently uses OTLP/HTTP exporter configuration. The deployment must provide any collector or tracing backend outside the default Compose stack. Exporter setup errors can fail startup only when tracing is explicitly enabled and misconfigured; transient export failures should not change normal API, worker, or judge execution behavior.

Do not put secrets, DSNs, JWTs, source code, testcase content, or full object keys in trace attributes. The implementation uses bounded route, queue, backend, phase, result, class, language, request, and task identifiers for operator pivots.

## Pivot From Metric To Trace Or Attempt

Use this workflow when an alert fires:

1. Identify the symptom panel and labels: `job`, `route`, `queue`, `dependency`, `backend`, `phase`, `class`, `scope`, or `language`.
2. Narrow the time window to the alert firing interval plus a few minutes before it.
3. Check logs for the same process and bounded identifiers. API responses include `request_id`; judge attempts expose `trace_id` through admin diagnostics.
4. If tracing is enabled, search the tracing backend for the process service name and time window. For API symptoms, start from the route span and request ID. For worker or judge-agent symptoms, search by the persisted `trace_id` when available.
5. Follow the trace across API, worker dispatch, Redis request consumption, judge-agent processing, sandbox phases, result publish, result consumer, and final persistence.
6. If the task reached `dead`, inspect persisted attempt diagnostics and `judge_tasks.last_error`, fix the underlying dependency, then use the documented dead-task recovery flow.

When tracing is disabled, `trace_id` remains an operator correlation value in judge attempts, but there is no exported trace to query. Use Prometheus labels, logs, Redis stream state, and persisted attempt diagnostics instead.

## Common Diagnosis Paths

Queue stuck:

- Start with `soj_queue_depth` and `soj_queue_oldest_pending_age_seconds`.
- If `request` is stuck, check judge-agent readiness, slot utilization, and sandbox errors.
- If `result` is stuck, check `soj_worker_result_consumer_messages_total{result="error"}` and database readiness.
- Pivot to the trace or persisted `trace_id` for a representative submission.

Result events missing:

- Check dispatch success rate and `request` queue depth.
- Check judge-agent readiness and slot usage.
- Check sandbox backend errors by `backend`, `phase`, and `class`.
- Use trace spans to see whether execution reached result publish.

Sandbox errors increasing:

- Start with `soj_sandbox_backend_errors_total` and `soj_sandbox_phase_duration_seconds`.
- Check runner image availability, Docker/runtime configuration, object storage access, and cleanup failures.
- Compare with recent deployment or runner image changes before recovering dead tasks.

Readiness failing:

- Use `soj_readiness_checks_total{result="error"}` to identify the dependency.
- Check p95 readiness duration for early degradation.
- Keep HTTP response bodies generic; use metrics and logs for dependency detail.

Unexpected verdict diagnostics:

- Compare the attempt manifest fields: judge core version, sandbox backend/profile, language runtime, testcase set hash, checker hash, and `trace_id`.
- If tracing is enabled, open the trace by `trace_id` and inspect sandbox phase spans.
- Do not infer scoreboard or retry behavior from dashboard panels; PostgreSQL remains the source of truth for terminal state.
