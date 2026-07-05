# SOJ v2 Docker Deployment

The first supported deployment target is Docker Compose. The v2 runtime is built and verified with Go 1.24.

```bash
docker compose -f deploy/docker-compose.yaml up --build -d
./deploy/smoke.sh
```

The Compose stack starts PostgreSQL, Redis, MinIO, a one-shot migration job, a local seed job, API, worker, judge-agent, and Prometheus. Local development uses the internal `fake://accepted` engine and seeds one enabled fake language so the submit/worker smoke flow can run before a privileged judge sandbox is available.

Production should run `soj-judge-agent` behind the async judge event boundary. Do not reuse the local fake language seed as production language data.

## Files

- `Dockerfile.v2`: multi-stage image for `soj-api`, `soj-worker`, `soj-judge-agent`, and `soj-migrate`.
- `deploy/docker-compose.yaml`: local v2 stack.
- `deploy/prometheus.yml`: local Prometheus scrape config for API and worker metrics.
- `deploy/env/api.env.example`: environment variable reference.
- `deploy/smoke.sh`: end-to-end local smoke test against the running stack.
- `deploy/config.example.yaml`: human-readable config reference. The current runtime reads `SOJ_*` environment variables.

## Required Secrets

Set these through environment variables in real deployments:

- `SOJ_DATABASE_DSN`
- `SOJ_JWT_SECRET`
- `SOJ_STORAGE_ACCESS_KEY`
- `SOJ_STORAGE_SECRET_KEY`

Do not commit production DSNs, JWT secrets, or object storage credentials.

## Health Checks

- API liveness: `GET /healthz`
- API readiness: `GET /readyz`
- Worker liveness: `GET /healthz`
- Worker readiness: `GET /readyz`
- Judge agent liveness: `GET /healthz`
- Judge agent readiness: `GET /readyz`

Current API readiness checks PostgreSQL. Worker readiness is process-level only; Redis/object storage/judge readiness is exercised by `deploy/smoke.sh` and worker logs. Add broader dependency probes before using these health checks as production traffic gates.

## Metrics

- API metrics: `GET http://localhost:8080/metrics`
- Worker metrics: `GET http://localhost:8081/metrics`
- Judge agent metrics: `GET http://localhost:8082/metrics`
- Prometheus UI: `http://localhost:9090`

The local Prometheus service scrapes `api:8080`, `worker:8081`, and `judge-agent:8082`. Current application metrics include HTTP request counts and latency, judge task dispatch counts, judge task processing counts, and judge task processing latency. Keep `/metrics` on a private network in production or protect it at the ingress layer.

Distributed tracing is not enabled yet. The intended next step is OpenTelemetry with OTLP export, disabled by default and switchable by environment.

## Judge Sandbox

Local Docker uses `SOJ_JUDGE_ENDPOINT=fake://accepted` and `SOJ_JUDGE_SANDBOX_BACKEND=fake` while the real sandbox path is being built.

For production-like real code execution:

- set `SOJ_JUDGE_SANDBOX_BACKEND=isolate`
- install and validate `isolate` inside the judge-agent runtime
- do not set `SOJ_JUDGE_SANDBOX_BACKEND=process` outside `dev`, `test`, or `local`
- keep judge-agent isolated from business database credentials

The process backend exists only for development tests and is rejected in non-development environments.

## Local Reset

```bash
docker compose -f deploy/docker-compose.yaml down -v --remove-orphans
docker compose -f deploy/docker-compose.yaml up --build -d
./deploy/smoke.sh
```

`down -v` deletes PostgreSQL, Redis, and MinIO volumes.
