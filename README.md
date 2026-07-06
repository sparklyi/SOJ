# SOJ

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

English | [简体中文](README.zh-CN.md)

SOJ is an open-source online judge backend written in Go. The active codebase is the v2 architecture: a focused REST API, asynchronous judge pipeline, PostgreSQL-backed domain model, Redis Stream delivery, S3-compatible object storage, and a separate judge-agent boundary for running code safely.

The historical v1 implementation is preserved on the `archive/v1` branch. New development should target the v2 `cmd/`, `internal/`, `api/`, `docs/`, and `deploy/` paths.

## Table of Contents

- [Features](#features)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Development](#development)
- [Configuration](#configuration)
- [API Documentation](#api-documentation)
- [GitHub Workflow](#github-workflow)
- [Project Layout](#project-layout)
- [Deployment Notes](#deployment-notes)
- [Roadmap](#roadmap)
- [License](#license)
- [Contact](#contact)

## Features

- User registration, login, refresh tokens, current-user profile, and admin user management.
- Problem metadata, statements, tags, testcase archive upload, publish checks, and problem statistics.
- Formal submissions, self-runs, judge tasks, async judge request/result streams, retries, dead-letter handling, and reconciliation loops.
- ACM contests, registration, contest submission policy, and live/frozen/final scoreboard responses.
- PostgreSQL migrations, `sqlc`/`pgx` data access, and an OpenAPI contract for frontend integration.
- Prometheus metrics, trial alert rules, and optional OpenTelemetry tracing for API, worker, and judge-agent processes.
- Local Docker Compose stack with PostgreSQL, Redis, MinIO, API, worker, judge-agent, migration, seed, Prometheus, and smoke testing.

## Architecture

SOJ v2 splits the backend into independent commands:

- `soj-api`: Gin HTTP transport and REST API.
- `soj-worker`: judge task dispatcher, result consumer, retry handling, and reconciliation loops.
- `soj-judge-agent`: async judge agent that consumes judge requests and publishes judge results.
- `soj-migrate`: versioned PostgreSQL migration runner.

Gin stays at the transport boundary. Business services receive `context.Context` and explicit `auth.Actor` values. PostgreSQL is the source of truth for submissions, runs, judge attempts, judge tasks, and contest results. Redis Stream messages are delivery hints and may be duplicated; terminal writes are designed to be idempotent.

Runtime dependencies:

- PostgreSQL: primary relational data store.
- Redis: judge request/result streams and consumer group coordination.
- MinIO/S3: source code, testcase archives, and future large artifacts.
- JudgeCore: compile/run/check pipeline with language profiles, checker logic, and sandbox adapters.
- Prometheus: local metrics scraping for API, worker, and judge-agent processes.
- OpenTelemetry: optional OTLP trace export, disabled by default.

For more detail, read [docs/v2-architecture.md](docs/v2-architecture.md).

## Quick Start

### Requirements

- Docker with Compose v2
- Go 1.25 for local development
- `curl`, `jq`, `zip`, and `shasum` for the smoke test
- Optional: GitHub CLI `gh` for issue, pull request, and repository operations

Start a clean local stack:

```bash
make down
make up
make smoke
```

Equivalent raw commands:

```bash
docker compose -f deploy/docker-compose.yaml down -v --remove-orphans
docker compose -f deploy/docker-compose.yaml up --build -d
./deploy/smoke.sh
```

Local services:

| Service | URL |
| --- | --- |
| API | `http://localhost:8080` |
| Worker health and metrics | `http://localhost:8081` |
| Judge-agent health and metrics | `http://localhost:8082` |
| MinIO console | `http://localhost:9001` |
| Prometheus | `http://localhost:9090` |

The local stack uses `fake://accepted` and `SOJ_JUDGE_SANDBOX_BACKEND=fake` so the full async judge flow can run before a privileged sandbox runtime is available.

To run the local real-code smoke path with the development-only process backend:

```bash
SOJ_ENV=local SOJ_JUDGE_ENDPOINT=agent://local SOJ_JUDGE_SANDBOX_BACKEND=process make up
SMOKE_REAL_JUDGE=1 make smoke
```

The process backend is useful for local validation, but it is not a production sandbox.

To run the local real-code smoke path through Docker runner containers:

```bash
make smoke-real-docker
```

`make smoke-real-docker` pulls the published runner images from GHCR by default. To build runner images locally while changing Dockerfiles, run:

```bash
RUNNER_IMAGES_PREPARE=build make smoke-real-docker
```

Runner images are published by [publish-runner-images.yml](.github/workflows/publish-runner-images.yml) when runner image files change on `main`, on version tags, and by manual workflow dispatch.

To validate the same path through gVisor/runsc after installing runsc:

```bash
./scripts/dev/install-gvisor.sh
make smoke-real-gvisor
```

Judge runtime readiness, recovery operations, and local validation evidence are documented in [docs/judge-runtime-readiness.md](docs/judge-runtime-readiness.md).

Trial dashboard queries, alert interpretation, and metric-to-trace diagnosis are documented in [docs/observability-trial-loop.md](docs/observability-trial-loop.md). The default stack does not require Grafana, Alertmanager, Jaeger, Tempo, or an OpenTelemetry collector.

The Docker runner path uses [deploy/docker-compose.docker-runner.yaml](deploy/docker-compose.docker-runner.yaml). Only `soj-judge-agent` receives the Docker socket; runner containers do not receive business service credentials or the Docker socket.

## Development

Run the main checks:

```bash
make test
make vet
make compose-config
make compose-config-docker-runner
```

Or run Go commands directly:

```bash
go test ./...
go vet ./...
docker compose -f deploy/docker-compose.yaml config
```

Useful local commands:

```bash
go run ./cmd/soj-api
go run ./cmd/soj-worker
go run ./cmd/soj-judge-agent
go run ./cmd/soj-migrate --help
```

The Docker smoke test validates registration, problem creation, statement upload, testcase archive upload, publication, async judging, contest registration, contest submission, scoreboard aggregation, metrics, and judge result stream persistence.

## Configuration

The runtime is configured through `SOJ_*` environment variables. See [deploy/env/api.env.example](deploy/env/api.env.example) and [deploy/config.example.yaml](deploy/config.example.yaml) for complete local examples.

Important variables:

| Variable | Purpose |
| --- | --- |
| `SOJ_ENV` | Runtime environment name, defaults to `dev`. |
| `SOJ_HTTP_ADDR` | API listen address, defaults to `:8080`. |
| `SOJ_WORKER_HEALTH_ADDR` | Worker health server address, defaults to `:8081`. |
| `SOJ_DATABASE_DSN` | PostgreSQL connection string. Required outside trivial local tests. |
| `SOJ_REDIS_ADDR` | Redis address. |
| `SOJ_REDIS_STREAM` | Judge request stream, defaults to `soj:judge:tasks`. |
| `SOJ_REDIS_GROUP` | Worker consumer group, defaults to `judge-workers`. |
| `SOJ_STORAGE_ENDPOINT` | S3-compatible object storage endpoint. |
| `SOJ_STORAGE_BUCKET` | Object storage bucket. |
| `SOJ_STORAGE_ACCESS_KEY` | Object storage access key. |
| `SOJ_STORAGE_SECRET_KEY` | Object storage secret key. |
| `SOJ_JWT_SECRET` | JWT signing secret. Must be changed for real deployments. |
| `SOJ_JUDGE_ENDPOINT` | Judge endpoint, such as `fake://accepted` or `agent://local`. |
| `SOJ_JUDGE_TIMEOUT` | Judge timeout, defaults to `30s`. |
| `SOJ_JUDGE_SANDBOX_BACKEND` | Judge-agent sandbox backend: `fake`, `process`, or `docker`. |
| `SOJ_JUDGE_PARALLELISM` | Global judge-agent sandbox slots. |
| `SOJ_JUDGE_LANGUAGE_SLOTS` | Per-language slot limits, such as `go=4,cpp17=4`. |
| `SOJ_DOCKER_RUNNER_RUNTIME` | Docker runtime for runner containers; production should use `runsc`. |
| `SOJ_DOCKER_RUNNER_IMAGE_GO` | Go runner image, defaults to `ghcr.io/sparklyi/soj-runner-go:main` for local smoke. |
| `SOJ_DOCKER_RUNNER_IMAGE_CPP17` | C++17 runner image, defaults to `ghcr.io/sparklyi/soj-runner-cpp17:main` for local smoke. |
| `SOJ_TRACING_ENABLED` | Enables OpenTelemetry tracing when set to `true`; defaults to disabled. |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | OTLP/HTTP trace endpoint, such as `http://collector:4318/v1/traces`. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Generic OTLP endpoint fallback when the traces endpoint is not set. |
| `OTEL_SERVICE_NAME` | Optional OpenTelemetry service name override. |
| `OTEL_RESOURCE_ATTRIBUTES` | Optional OpenTelemetry resource attributes, such as deployment environment. |

## API Documentation

- OpenAPI contract: [api/openapi.yaml](api/openapi.yaml)
- API guide: [docs/v2-api-guide.md](docs/v2-api-guide.md)
- Docker deployment: [docs/v2-deploy.md](docs/v2-deploy.md)
- Worker operations: [docs/v2-worker.md](docs/v2-worker.md)
- Observability trial loop: [docs/observability-trial-loop.md](docs/observability-trial-loop.md)

Endpoint groups:

- Auth: registration, login, refresh, logout, current user.
- Problems: list, create, update, statements, testcase sets, stats.
- Submissions and runs: formal submissions, self-runs, result visibility.
- Contests: contest CRUD, registration, live/frozen/final scoreboards.
- Admin: user administration and judge language management.

All successful JSON `2xx` responses use a response envelope unless the endpoint explicitly returns an empty `204` or `202`.

## GitHub Workflow

GitHub CLI is recommended for day-to-day repository work:

```bash
gh auth login
gh repo view
gh issue list
gh pr list
```

Create a branch and pull request:

```bash
git checkout -b codex/update-readme
git add README.md README.zh-CN.md
git commit -m "docs: refresh readme"
git push -u origin codex/update-readme
gh pr create --fill
```

Install `gh` on Ubuntu if needed:

```bash
sudo apt-get update
sudo apt-get install -y gh
```

## Project Layout

```text
api/                    OpenAPI contract
cmd/soj-api             HTTP API entrypoint
cmd/soj-judge-agent     Judge agent entrypoint
cmd/soj-worker          Judge worker entrypoint
cmd/soj-migrate         PostgreSQL migration entrypoint
deploy/                 Docker Compose, env examples, Prometheus, smoke test
docs/                   v2 architecture, API guide, worker and deploy docs
internal/app            Runtime assembly for commands
internal/auth           Actor, JWT, password, token primitives
internal/user           Account and admin user use cases
internal/problem        Problems, statements, tags, testcase sets
internal/submission     Submissions, runs, judge tasks, worker logic
internal/judge          Judge protocol and async event contracts
internal/judgecore      Judge core pipeline, language profiles, checker, sandbox adapters
internal/contest        ACM contests, registrations, scoreboards
internal/postgres       SQL queries and generated sqlc code
internal/queue          Redis Stream task queue
internal/storage        S3-compatible object storage
internal/observability  Logging, health checks, Prometheus metrics, and tracing setup
```

## Deployment Notes

The supported local deployment is [deploy/docker-compose.yaml](deploy/docker-compose.yaml).

Before exposing SOJ outside local development:

- Replace all local credentials and use a strong `SOJ_JWT_SECRET`.
- Use production PostgreSQL, Redis, and S3-compatible object storage credentials.
- Keep `/metrics` on a private network or protect it at the ingress layer.
- Keep tracing backends and collectors private; tracing is off by default and must be enabled with `SOJ_TRACING_ENABLED=true`.
- Run `soj-judge-agent` without business database credentials.
- Treat `SOJ_JUDGE_SANDBOX_BACKEND=docker` with Docker runtime `runsc`/gVisor as the production sandbox target.
- Set `SOJ_ENV=prod` and `SOJ_DOCKER_RUNNER_RUNTIME=runsc` on production judge nodes; startup fails if runsc or the no-op runner probe is unavailable.
- Pin `SOJ_DOCKER_RUNNER_IMAGE_GO` and `SOJ_DOCKER_RUNNER_IMAGE_CPP17` to release or `sha-*` runner image tags in production.
- Make GHCR runner packages public or log in to `ghcr.io` on private judge nodes before pulling images.
- Do not use the `process` sandbox backend outside development, tests, and local real-code smoke.
- Do not reuse the local fake language seed as production language data.

## Roadmap

Known follow-up work:

- Production-specific dashboard and alert threshold tuning after trial traffic baselines are known.
- Reusable Grafana JSON after the dashboard query set stabilizes.

## License

SOJ is released under the [MIT License](LICENSE).

## Contact

- WeChat: `sparkyi1026`
- Email: `sparkyi@foxmail.com`
