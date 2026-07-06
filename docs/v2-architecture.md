# SOJ v2 Architecture

SOJ v2 targets Go 1.25 and splits the backend into four commands:

- `soj-api`: Gin HTTP transport and REST API.
- `soj-worker`: judge task dispatcher, Redis Stream consumer, and reconciliation loops.
- `soj-judge-agent`: isolated async judge request processor.
- `soj-migrate`: versioned PostgreSQL migration runner.

Gin is kept at the transport boundary. Business services receive `context.Context` plus explicit `auth.Actor`; repositories use PostgreSQL through sqlc/pgx. Redis Stream coordinates judge task delivery, but PostgreSQL remains the source of truth for submissions, runs, and judge tasks.

## Runtime Dependencies

- PostgreSQL: primary relational store.
- Redis: judge task stream and consumer group coordination.
- MinIO/S3: source files, testcase archives, and future large artifacts.
- JudgeEngine: protocol boundary for the internal fake engine and the future `soj-judge-agent` process.

## Module Boundaries

- `internal/auth` owns actors, token logic, password hashing, and refresh token policy.
- `internal/user` owns account and admin user use cases.
- `internal/problem` owns problem metadata, statements, testcase sets, and publish rules.
- `internal/submission` owns submissions, self-runs, judge tasks, and worker orchestration.
- `internal/contest` owns ACM contests and scoreboards.
- `internal/httpapi` owns request binding, response envelopes, and Gin route registration conventions.

## Deployment Boundary

The supported local deployment is `deploy/docker-compose.yaml`. It runs migration and seed as one-shot jobs before API/worker startup, then validates the core workflow through `deploy/smoke.sh`.

## Observability Boundary

API, worker, and judge-agent processes expose Prometheus metrics at `/metrics`. The API records HTTP request counts and latency by method, normalized route, and status. The worker records judge task dispatch counts, result-consumer counts and latency, queue depth/pending age, judge task outcomes, recovery activity, and reconciliation activity. The judge-agent records slot capacity/usage, sandbox phase latency, sandbox backend errors, and cleanup failures.

Prometheus alert rules are checked in under `deploy/prometheus-rules/soj-alerts.yml` and loaded by the local Prometheus configuration. Dashboard queries are documented in `docs/observability-trial-loop.md` instead of requiring a default Grafana service.

Distributed tracing uses OpenTelemetry and OTLP/HTTP export when explicitly enabled with `SOJ_TRACING_ENABLED=true`. It remains disabled by default even if generic `OTEL_*` variables are present. Trace context is carried across HTTP, worker dispatch, Redis judge request/result events, judge-agent execution, sandbox phases, result consumption, and final persistence. Existing API `request_id` values and persisted judge attempt `trace_id` values remain the operator pivots.

## Legacy Boundary

The v1 implementation is archived on the `archive/v1` branch. Main keeps only the v2 backend path and the old root Dockerfile, root Compose file, MySQL/Gorm/Mongo/RabbitMQ modules, and v1 HTTP wiring are removed from the active tree.
