# SOJ v2 Docker Deployment

The first supported deployment target is Docker Compose. The v2 runtime is built and verified with Go 1.24.

```bash
docker compose -f deploy/docker-compose.yaml up --build -d
./deploy/smoke.sh
```

The Compose stack starts PostgreSQL, Redis, MinIO, a one-shot migration job, a local seed job, API, worker, judge-agent, and Prometheus. Local development uses the internal `fake://accepted` engine and seeds one enabled fake language so the submit/worker/agent/result-consumer smoke flow can run before a privileged judge sandbox is available.

Production should run `soj-judge-agent` behind the async judge event boundary. Do not reuse the local fake language seed as production language data.

For a local real-code smoke path, use the development-only process backend:

```bash
SOJ_ENV=local \
SOJ_JUDGE_ENDPOINT=agent://local \
SOJ_JUDGE_SANDBOX_BACKEND=process \
docker compose -f deploy/docker-compose.yaml up --build -d

SMOKE_REAL_JUDGE=1 ./deploy/smoke.sh
```

This mode runs real Go/C++ toolchains in the judge-agent container, but it is not a production sandbox.

## Judge Event Flow

The worker process has two production loops: a dispatcher that claims `judge_tasks` and publishes `judge.request.v1` to `SOJ_REDIS_STREAM`, and a result consumer that reads `judge.result.v1` from `SOJ_JUDGE_RESULT_STREAM`. The result consumer acknowledges Redis only after the PostgreSQL transaction updates `judge_attempts`, `submission_results`, `judge_tasks`, and contest projections.

`soj-judge-agent` consumes requests from `SOJ_JUDGE_REQUEST_STREAM` and publishes results to `SOJ_JUDGE_RESULT_STREAM`. The agent does not receive business database credentials. The result consumer group is created from Redis stream ID `0` so already-published result events are not skipped during first startup or recovery.

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

## Judge Agent Credential Boundary

`soj-judge-agent` consumes Redis request events, reads source/testcase artifacts from object storage, and publishes Redis result events. It must not receive business PostgreSQL credentials. The worker result consumer owns all business database writes for attempts, case results, submission projections, and contest projections.

## Health Checks

- API liveness: `GET /healthz`
- API readiness: `GET /readyz`
- Worker liveness: `GET /healthz`
- Worker readiness: `GET /readyz`
- Judge agent liveness: `GET /healthz`
- Judge agent readiness: `GET /readyz`

Current readiness checks:

- API readiness checks PostgreSQL.
- Worker readiness checks PostgreSQL, Redis request stream, Redis result stream, and object storage.
- Judge-agent readiness checks Redis request stream, Redis result stream, object storage, and the configured sandbox backend probe.

See `docs/judge-runtime-readiness.md` for the operational checklist, recovery command, metrics, and local validation environment.

## Metrics

- API metrics: `GET http://localhost:8080/metrics`
- Worker metrics: `GET http://localhost:8081/metrics`
- Judge agent metrics: `GET http://localhost:8082/metrics`
- Prometheus UI: `http://localhost:9090`

The local Prometheus service scrapes `api:8080`, `worker:8081`, and `judge-agent:8082`. Current application metrics include HTTP request counts and latency, judge task dispatch counts, judge-agent slot usage, sandbox phase duration, sandbox backend errors, and sandbox cleanup failures. Keep `/metrics` on a private network in production or protect it at the ingress layer.

Judge-agent runner metrics to watch during Docker/gVisor rollout:

- `soj_judge_agent_slots_used` and `soj_judge_agent_slots_capacity`
- `soj_sandbox_phase_duration_seconds`
- `soj_sandbox_backend_errors_total`
- `soj_sandbox_cleanup_failures_total`

Distributed tracing is not enabled yet. The intended next step is OpenTelemetry with OTLP export, disabled by default and switchable by environment.

## Judge Sandbox

Local Docker uses `SOJ_JUDGE_ENDPOINT=fake://accepted` and `SOJ_JUDGE_SANDBOX_BACKEND=fake` for the fastest deterministic smoke path.

Real local Docker runner validation:

```bash
make smoke-real-docker
```

The smoke target pulls published GHCR runner images by default. Use `RUNNER_IMAGES_PREPARE=build make smoke-real-docker` only when developing runner Dockerfiles locally.

Runner images are published by `.github/workflows/publish-runner-images.yml` when runner image files change on `main`, on version tags, and by manual workflow dispatch.

Real local gVisor/runsc validation:

```bash
./scripts/dev/install-gvisor.sh
make smoke-real-gvisor
```

Backend safety matrix:

| Backend | Allowed environments | Purpose |
| --- | --- | --- |
| `fake` | local, dev, CI smoke | Deterministic async pipeline validation. It does not execute user code. |
| `process` | `dev`, `test`, `local` only | Development-only real code execution with wall-time and output guards. It is not a security sandbox. |
| `docker` | production target | Docker runner backend with gVisor/runsc in production. Development may explicitly allow the default Docker runtime for local smoke only. |
| `isolate` | future backend | Reserved host sandbox adapter behind the same sandbox contract. It is not the current production mainline. |

For production real code execution:

- set `SOJ_JUDGE_SANDBOX_BACKEND=docker`
- set `SOJ_ENV=prod`
- install and validate Docker plus gVisor/runsc on the judge node
- configure `SOJ_DOCKER_RUNNER_RUNTIME=runsc` for production
- pin `SOJ_DOCKER_RUNNER_IMAGE_GO` and `SOJ_DOCKER_RUNNER_IMAGE_CPP17` to release or `sha-*` tags
- make GHCR runner packages public or run `docker login ghcr.io` on private judge nodes before pulling images
- do not set `SOJ_JUDGE_SANDBOX_BACKEND=process` outside `dev`, `test`, or `local`
- keep judge-agent isolated from business database credentials

Production startup runs a Docker capability probe. It fails if Docker is unavailable, the configured runtime is not `runsc`, the `runsc` runtime is not registered, the runner image is missing, or a no-op runner container cannot start with no network, read-only rootfs, dropped capabilities, `no-new-privileges`, and non-root user.

Single-node deployment can run API, worker, Redis, PostgreSQL, object storage, and one judge-agent on the same host for small installations. The judge-agent still needs a dedicated Docker runner work directory and Docker socket access, and runner containers must not mount the Docker socket.

Multi-node deployment runs additional `soj-judge-agent` processes on dedicated judge nodes. They share Redis request/result streams and object storage with the main stack, use the same runner images, and should set `SOJ_JUDGE_PARALLELISM` and `SOJ_JUDGE_LANGUAGE_SLOTS` according to host CPU and memory.

The process backend exists only for development tests and local real-code smoke. It is rejected in non-development environments.

## Troubleshooting

- Queue backlog: check Redis stream length for `SOJ_REDIS_STREAM`, worker logs, and `soj_worker_judge_task_dispatch_total`.
- No result events: check judge-agent readiness, `SOJ_JUDGE_REQUEST_STREAM`, `SOJ_JUDGE_RESULT_STREAM`, and object storage credentials.
- Agent startup failure: verify `SOJ_REDIS_ADDR`, object storage credentials, Docker socket access, runner images, `SOJ_JUDGE_SANDBOX_BACKEND`, and `SOJ_DOCKER_RUNNER_RUNTIME` safety rules.
- Sandbox verdict anomalies: compare the attempt manifest fields for judge core version, sandbox backend/profile, language runtime, testcase set hash, and trace id.
- Local Docker runner smoke fails with wrong answers on input-reading programs: confirm Docker run uses the current code and `--interactive` is present in the runner args.
- Local real smoke fails with compile errors: confirm runner images exist with `make runner-images-pull`, or use `RUNNER_IMAGES_PREPARE=build` while developing Dockerfiles locally.
- Capacity smoke below target: compare `container_startup_p95_ms`, `p95_attempt_ms`, queue oldest pending age, and `soj_sandbox_backend_errors_total` before raising slots or adding judge-agent nodes.

## Local Reset

```bash
docker compose -f deploy/docker-compose.yaml down -v --remove-orphans
docker compose -f deploy/docker-compose.yaml up --build -d
./deploy/smoke.sh
```

`down -v` deletes PostgreSQL, Redis, and MinIO volumes.
