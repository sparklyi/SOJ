# SOJ

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev/)
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
- Prometheus metrics for API, worker, and judge-agent processes.
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

For more detail, read [docs/v2-architecture.md](docs/v2-architecture.md).

## Quick Start

### Requirements

- Docker with Compose v2
- Go 1.24 for local development
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

## Development

Run the main checks:

```bash
make test
make vet
make compose-config
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

## API Documentation

- OpenAPI contract: [api/openapi.yaml](api/openapi.yaml)
- API guide: [docs/v2-api-guide.md](docs/v2-api-guide.md)
- Docker deployment: [docs/v2-deploy.md](docs/v2-deploy.md)
- Worker operations: [docs/v2-worker.md](docs/v2-worker.md)

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
internal/observability  Logging, health checks, and Prometheus metrics
```

## Deployment Notes

The supported local deployment is [deploy/docker-compose.yaml](deploy/docker-compose.yaml).

Before exposing SOJ outside local development:

- Replace all local credentials and use a strong `SOJ_JWT_SECRET`.
- Use production PostgreSQL, Redis, and S3-compatible object storage credentials.
- Keep `/metrics` on a private network or protect it at the ingress layer.
- Run `soj-judge-agent` without business database credentials.
- Treat `SOJ_JUDGE_SANDBOX_BACKEND=isolate` as the production sandbox target once the isolate adapter is completed and validated.
- Do not use the `process` sandbox backend outside development, tests, and local real-code smoke.
- Do not reuse the local fake language seed as production language data.

## Roadmap

Known follow-up work:

- Automated frozen/final scoreboard snapshot generation in the worker.
- Production-grade judge sandbox image and isolate runtime validation.
- Broader readiness probes for worker dependencies.
- OpenTelemetry tracing with OTLP export disabled by default.

## License

SOJ is released under the [MIT License](LICENSE).

## Contact

- WeChat: `sparkyi1026`
- Email: `sparkyi@foxmail.com`
