# Judge Runtime Readiness

This document is the operational checklist for running the SOJ judge runtime in a trial deployment. It covers readiness probes, dead-task recovery, runtime metrics, and the local validation environment used for this stage.

## Readiness Probes

All services keep `GET /healthz` as liveness and `GET /readyz` as dependency readiness.

| Process | Readiness dependencies |
| --- | --- |
| `soj-api` | PostgreSQL |
| `soj-worker` | PostgreSQL, Redis request stream, Redis result stream, object storage bucket |
| `soj-judge-agent` | Redis request stream, Redis result stream, object storage bucket, sandbox backend probe |

Readiness failures return HTTP 503 with a generic response. Dependency names and failure counts are exposed through metrics rather than HTTP response bodies, so secrets, DSNs, bucket names, and internal addresses are not leaked.

Local checks:

```bash
curl -fsS http://localhost:8080/readyz
curl -fsS http://localhost:8081/readyz
curl -fsS http://localhost:8082/readyz
```

## Dead Task Recovery

The worker writes exhausted judge tasks to PostgreSQL as `judge_tasks.status = 'dead'` and writes a diagnostic Redis dead-letter entry. PostgreSQL remains the recovery source of truth.

Recover one dead task:

```bash
docker compose -f deploy/docker-compose.yaml -f deploy/docker-compose.docker-runner.yaml \
  exec worker /app/soj recover-dead-task \
  -task-id 123 \
  -reason "manual recovery after runner fix"
```

Recovery rules:

- The task must still be `dead`.
- The linked submission must still be `system_error`.
- The task is reset to `pending`.
- The task retry budget is reset with `attempts = 0`.
- `next_run_at` is set to the recovery time.
- The submission is reset to `queued` and `judged_at = NULL`.
- Redis dead-letter entries remain as diagnostic evidence; they are not the replay source.

## Metrics

Runtime readiness and recovery metrics:

- `soj_readiness_checks_total{service,dependency,result}`
- `soj_readiness_check_duration_seconds{service,dependency,result}`
- `soj_worker_reconciliation_total{service,action,result}`
- `soj_worker_judge_task_recovery_total{service,action,result}`

Existing judge runtime metrics to keep on dashboards:

- `soj_worker_judge_task_dispatch_total{result}`
- `soj_worker_judge_tasks_total{result}`
- `soj_worker_judge_task_duration_seconds{result}`
- `soj_judge_agent_slots_used{scope,language}`
- `soj_judge_agent_slots_capacity{scope,language}`
- `soj_sandbox_phase_duration_seconds{backend,phase,result}`
- `soj_sandbox_backend_errors_total{backend,phase,class}`
- `soj_sandbox_cleanup_failures_total{backend}`

Local Prometheus is available at `http://localhost:9090` when the Compose stack is running.

## Local Validation Environment

The 2026-07-06 validation used:

- macOS Darwin 25.5.0 arm64
- Apple M5, 10 CPU cores, 24 GiB memory
- Docker context: `orbstack`
- Docker client/server: `29.4.0`
- Docker runtimes: `runc`; `runsc` was not registered
- Runner images:
  - `ghcr.io/sparklyi/soj-runner-go:main@sha256:148de7dcab3eada409f7a590a998d2b3123cd955a59029b2dadcdce238902e11`
  - `ghcr.io/sparklyi/soj-runner-cpp17:main@sha256:60025cca9d106bc45b7c02cdb899b56a2a5561be58497746471f9f0b0f786c31`

Validation commands:

```bash
go test ./...
go vet ./...
docker compose -f deploy/docker-compose.yaml config
RUNNER_IMAGES_PREPARE=pull make smoke-real-docker
SOJ_DOCKER_RUNNER_RUNTIME=runsc RUNNER_IMAGES_PREPARE=skip make smoke-runner-capacity
RUNNER_IMAGES_PREPARE=skip SOJ_CAPACITY_SKIP_BUILD=1 make smoke-runner-capacity
```

Results are recorded in [runner-capacity-report-2026-07-06.md](runner-capacity-report-2026-07-06.md).

## Troubleshooting

- `/readyz` fails for worker: check PostgreSQL connectivity, Redis stream/group creation, object storage bucket existence, and `soj_readiness_checks_total`.
- `/readyz` fails for judge-agent: check Redis streams, object storage credentials, sandbox backend probe, runner images, and Docker/runtime registration.
- Queue grows but no results arrive: check judge-agent readiness, `soj_judge_agent_slots_used`, Redis request/result streams, and sandbox backend errors.
- Dead tasks accumulate: inspect `judge_tasks.last_error`, Redis `soj:judge:tasks:dead`, and `soj_worker_reconciliation_total`; recover individual tasks only after fixing the underlying dependency.
- Capacity does not scale with more slots: compare `container_startup_p95_ms`, attempt latency, agent memory peak, queue oldest pending age, and sandbox error deltas.
