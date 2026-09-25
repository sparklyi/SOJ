# Sundial Online Judge (SOJ)

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/sparklyi/SOJ/actions/workflows/ci.yml/badge.svg)](https://github.com/sparklyi/SOJ/actions/workflows/ci.yml)

English | [简体中文](README.zh-CN.md)

**Sundial Online Judge** (SOJ) is an open-source online judge for practice, contests,
submissions, scoreboards, and live contest broadcast. This repository holds the Go
backend; the Next.js frontend lives in
[SOJ-web](https://github.com/sparklyi/SOJ-web).

| Repository | Role | Stack |
| --- | --- | --- |
| `SOJ` (this repository) | REST API, asynchronous judge pipeline, contests, scoreboards | Go 1.25, Gin, PostgreSQL, Redis Streams, S3/MinIO |
| [`SOJ-web`](https://github.com/sparklyi/SOJ-web) | Practice, contest, submission, and scoreboard UI | Next.js, TypeScript, Tailwind CSS |

## Table of Contents

- [Highlights](#highlights)
- [Architecture](#architecture)
- [Getting Started](#getting-started)
- [Judge Runtime](#judge-runtime)
- [Configuration](#configuration)
- [API](#api)
- [Development](#development)
- [Deployment](#deployment)
- [Project Layout](#project-layout)
- [Roadmap](#roadmap)
- [License](#license)
- [Links](#links)

## Highlights

- **Accounts**: registration, login, refresh tokens, profile, admin user management.
- **Problems**: metadata, statements, tags, testcase archive upload, publish checks, statistics.
- **Submissions and self-runs**: formal submissions, the problem page's run button and the
  playground, judge tasks, async request/result streams, retries, dead letters, reconciliation.
- **Contests**: ACM contests, registration, submission policy, live/frozen/final scoreboards.
- **Platform**: PostgreSQL migrations, `sqlc`/`pgx` data access, OpenAPI contract, Prometheus
  metrics, and optional OpenTelemetry tracing.
- **Local stack**: Docker Compose brings up PostgreSQL, Redis, MinIO, API, worker, judge-agent,
  migration, seed, Prometheus, and a smoke test.

## Architecture

SOJ splits the backend into four commands:

- `soj-api`: Gin HTTP transport and REST API.
- `soj-worker`: judge task dispatcher, result consumer, retry handling, reconciliation loops.
- `soj-judge-agent`: async judge agent that consumes judge requests and publishes judge results.
- `soj-migrate`: versioned PostgreSQL migration runner.

The judge path is asynchronous end to end: the API commits the task to PostgreSQL, publishes a
delivery hint to a Redis Stream, and returns; the worker routes the task to the judge-agent,
which compiles, runs, and checks the code inside its sandbox; results travel back over Redis
Streams and are consumed idempotently. PostgreSQL is the source of truth for submissions, runs,
judge attempts, judge tasks, and contest results. Redis Stream messages are delivery hints and
may be duplicated; terminal writes are designed to be idempotent.

Gin stays at the transport boundary. Business services receive `context.Context` and explicit
`auth.Actor` values.

Runtime dependencies:

- **PostgreSQL**: primary relational data store.
- **Redis**: judge request/result streams and consumer group coordination.
- **MinIO/S3**: source code, testcase archives, and future large artifacts.
- **JudgeCore**: compile/run/check pipeline with language profiles, checker logic, and sandbox adapters.
- **Prometheus**: local metrics scraping for API, worker, and judge-agent processes.
- **OpenTelemetry**: optional OTLP trace export, disabled by default.

For more detail, read [docs/v2-architecture.md](docs/v2-architecture.md).

## Getting Started

### Requirements

- Docker with Compose v2
- Go 1.25 for local backend development
- Node.js 22 and npm for the frontend
- `curl`, `jq`, `zip`, and `shasum` for the smoke test
- Optional: GitHub CLI `gh` for issue, pull request, and repository operations

### Backend: local stack

Start a clean stack and run the end-to-end smoke test:

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

The default stack runs `judge.sandbox_backend: fake` with `fake://accepted`, so the full
async judge flow runs before a privileged sandbox runtime is available.

### Backend: run processes directly

```bash
go run ./cmd/soj-migrate
go run ./cmd/soj-api
go run ./cmd/soj-worker
go run ./cmd/soj-judge-agent
```

`soj-migrate --help` lists the migration flags.

### Frontend

The frontend is a separate repository: [SOJ-web](https://github.com/sparklyi/SOJ-web).

```bash
git clone git@github.com:sparklyi/SOJ-web.git
cd SOJ-web
npm ci
cp .env.example .env.local   # defaults to mock data
npm run dev
```

Open `http://localhost:3000`. Key environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `NEXT_PUBLIC_SOJ_API_MODE` | local `mock`, production `http` | Selects the API adapter. |
| `NEXT_PUBLIC_SOJ_API_BASE_URL` | `/soj-api` in the browser | Public API base in `http` mode; unset uses the same-origin proxy. |
| `SOJ_API_INTERNAL_BASE_URL` | `http://localhost:8080` | Backend URL for server-side requests and the `/soj-api/*` rewrite. |

Browser calls go through the same-origin `/soj-api/*` proxy, so local CORS needs no setup.

## Judge Runtime

### Sandbox backends

| Backend | Where it runs | Use for |
| --- | --- | --- |
| `fake` | No execution, canned verdicts | Local stack, CI contract checks |
| `process` | Child processes inside the API or agent process | Local real-code validation only |
| `docker` | Runner containers via `soj-judge-agent` | Production target, with `runsc`/gVisor |

Only `soj-judge-agent` may hold a Docker socket; runner containers never receive the Docker
socket or business service credentials. `process` is not a production sandbox.

To run the real-code smoke path with the development-only process backend:

```bash
SOJ_ENV=local SOJ_JUDGE_ENDPOINT=agent://local SOJ_JUDGE_SANDBOX_BACKEND=process make up
SMOKE_REAL_JUDGE=1 make smoke
```

### Self-runs

**Self-runs** (the problem page's run button and the playground) go through the same async
pipeline as submissions: the API writes a judge task, the worker publishes it to the run
stream, and the judge-agent executes it. The default `agent://local` endpoint is therefore
enough, and it is the production path -- the API never runs untrusted code.

Runs travel on their own request stream (`redis.run_stream`) so playground traffic cannot
queue in front of a formal submission. By default one agent consumes both streams and shares
its sandbox slots; set `redis.agent_streams: runs` on a second agent to give the playground
its own capacity.

For single-node deployments and local development there is also `local://`, which compiles and
executes self-runs inside the API process:

```bash
SOJ_ENV=local SOJ_JUDGE_ENDPOINT=local:// SOJ_JUDGE_SANDBOX_BACKEND=process make up
```

It **refuses the `docker` backend**: only `soj-judge-agent` may hold a Docker socket. Prefer
the async path when worker and agent are running.

### Self-run retention

A self-run is scratch work, but every one of them writes a source object to storage. The worker
therefore sweeps finished self-runs past `retention.run_days` and removes the object before
the row, so a failure leaves the row for the next sweep instead of orphaning an object that
nothing could ever find again. Submissions are untouched: their source is needed for rejudge.

### Real-code smoke paths

Through Docker runner containers:

```bash
make smoke-real-docker
```

The target pulls each language's published runner image from GHCR by default. To build them locally while
changing Dockerfiles:

```bash
RUNNER_IMAGES_PREPARE=build make smoke-real-docker
```

Runner images are published by
[publish-runner-images.yml](.github/workflows/publish-runner-images.yml) when runner image files
change on `main`, on version tags, and by manual workflow dispatch.

Through gVisor/runsc, after installing runsc:

```bash
./scripts/dev/install-gvisor.sh
make smoke-real-gvisor
```

The Docker runner path uses
[deploy/docker-compose.docker-runner.yaml](deploy/docker-compose.docker-runner.yaml). Judge
runtime readiness, recovery operations, and local validation evidence are documented in
[docs/judge-runtime-readiness.md](docs/judge-runtime-readiness.md). Dashboard queries, alert
interpretation, and metric-to-trace diagnosis are documented in
[docs/observability-trial-loop.md](docs/observability-trial-loop.md); the default stack does not
require Grafana, Alertmanager, Jaeger, Tempo, or an OpenTelemetry collector.

## Configuration

[deploy/config.yaml](deploy/config.yaml) is the configuration. It carries the
full schema with the values the Compose stack uses; the defaults for every key
live in [internal/config](internal/config), and this file is what a deployment
overrides.

Values written as placeholders (`${NAME}` or `${NAME:-default}`) are resolved
from the environment when the file is read. That is the whole environment
interface: secrets and per-deployment knobs are declared in the file, and the
loader reads nothing else from the environment except the file's own path.

Every command accepts:

| Flag | Purpose |
| --- | --- |
| `--config <file>` | Configuration file. Defaults to `$SOJ_CONFIG_FILE`, then `./config.yaml` when it exists. |
| `--print-config` | Print the effective configuration with secrets masked, then exit. |

```bash
go run ./cmd/soj-api --config deploy/config.yaml --print-config
```

The shipped file references these variables
(`grep -o '\${[A-Z_]*' deploy/config.yaml` lists them straight from the file):

| Placeholder | Purpose |
| --- | --- |
| `SOJ_DATABASE_DSN` | PostgreSQL connection string; required by the api, worker, and migrate commands. |
| `SOJ_JWT_SECRET` | JWT signing secret; required by the api command. |
| `SOJ_STORAGE_ACCESS_KEY` / `SOJ_STORAGE_SECRET_KEY` | Object storage credentials. |
| `SOJ_ENV` | Environment name, defaults to `docker`. |
| `SOJ_JUDGE_ENDPOINT` / `SOJ_JUDGE_SANDBOX_BACKEND` | Judge routing and sandbox backend. |
| `SOJ_JUDGE_AGENT_STREAMS` | Streams a judge agent consumes: `all` (default), `submissions`, or `runs`. |
| `SOJ_JUDGE_PARALLELISM` / `SOJ_JUDGE_LANGUAGE_SLOTS` / `SOJ_JUDGE_MAX_BATCH` | Judge agent capacity. |
| `SOJ_JUDGE_RUN_PARALLELISM` / `SOJ_JUDGE_RUN_PER_USER` / `SOJ_JUDGE_RUN_STDIN_MAX_BYTES` | Self-run limits. |
| `SOJ_RUN_RETENTION_DAYS` / `SOJ_RUN_RETENTION_INTERVAL` / `SOJ_RUN_RETENTION_BATCH` | Self-run retention sweep. |
| `SOJ_RUNNER_REGISTRY` / `SOJ_RUNNER_TAG` | Registry and tag the language files resolve their runner images with. |
| `SOJ_DOCKER_RUNNER_*` | Runner runtime, workdir, and user for the docker sandbox. |
| `OTEL_*` | OpenTelemetry service name, resource attributes, and trace endpoint. |


### Languages

The judge runs the languages defined in `deploy/languages/`: one directory per
language, the directory name is the slug, and everything about the language lives
in its `language.yaml`.

```text
deploy/languages/
  go/
    language.yaml
    Dockerfile
  python3/
    language.yaml
    Dockerfile
```

| Key | Meaning |
| --- | --- |
| `name`, `version` | Catalog display values. |
| `source_file` | Where the submitted source is written in the workspace. |
| `binary_file` | What compilation produces; empty for interpreted languages. |
| `compile` | argv the sandbox runs to build the source, with `{{source}}` and `{{binary}}` placeholders. Empty skips compilation. |
| `run` | argv that executes once per testcase, with the same placeholders. |
| `image` | Runner image; `${SOJ_RUNNER_REGISTRY}` and `${SOJ_RUNNER_TAG}` fill in the deployment's registry and tag. |
| `time_limit_ms`, `memory_limit_kb` | Insert defaults for the catalog row; administrators tune the row afterwards. |

Adding a language means adding a directory: write `language.yaml`, add a
`Dockerfile` when it needs its own toolchain (the publish workflow discovers it),
and restart the API. The API reconciles the directory into the database at
startup, so no seed SQL and no schema change are involved. A language removed
from the directory is disabled, and enablement and limits set through the admin
API are never overwritten.

The directories are validated while they load: an unknown key, an unknown
placeholder, a missing `run`, or a `binary_file` without a `compile` all fail
startup naming the file and key.

Copy [deploy/env/api.env.example](deploy/env/api.env.example) to supply them.
The production overlay and its required variables are documented in
[docs/v2-deploy.md](docs/v2-deploy.md).

## API

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

All successful JSON `2xx` responses use a response envelope unless the endpoint explicitly
returns an empty `204` or `202`.

## Development

Run the main checks:

```bash
make test
make vet
make compose-config
make compose-config-docker-runner
```

Or run the underlying commands directly:

```bash
go test ./...
go vet ./...
docker compose -f deploy/docker-compose.yaml config
```

The Docker smoke test validates registration, problem creation, statement upload, testcase
archive upload, publication, async judging, contest registration, contest submission, scoreboard
aggregation, metrics, and judge result stream persistence.

Repository workflow: branch from `main`, keep commits focused and Conventional-Commit style,
and let CI cover `typecheck`-equivalent Go checks plus the Docker smoke stack. GitHub CLI is
recommended for day-to-day work:

```bash
gh auth login
gh issue list
gh pr list
```

## Deployment

The supported local deployment is [deploy/docker-compose.yaml](deploy/docker-compose.yaml).

Releases are tag-driven and the server never builds:

- Backend: pushing a `v*` tag builds the `soj-*` images in GitHub Actions and attaches them to
  a GitHub Release; the server polls for new tags, loads the images, and restarts the Compose
  stack with rollback on a failed health check.
- Frontend: pushing a `web-v*` tag publishes a standalone build; the server swaps versions
  behind an atomic symlink.

Before exposing SOJ outside local development:

- Replace all local credentials and use a strong `SOJ_JWT_SECRET`.
- Use production PostgreSQL, Redis, and S3-compatible object storage credentials.
- Keep `/metrics` on a private network or protect it at the ingress layer.
- Keep tracing backends and collectors private; tracing is off by default and must be enabled
  with `tracing.enabled: true`.
- Run `soj-judge-agent` without business database credentials.
- Treat `judge.sandbox_backend: docker` with the `runsc`/gVisor runtime as the production
  sandbox target.
- Set `env: prod` and `agent.runner.runtime: runsc` on production judge nodes; startup
  fails if runsc or the no-op runner probe is unavailable.
- Pin the runner images to release or `sha-*` tags with `SOJ_RUNNER_TAG`.
- Make GHCR runner packages public or log in to `ghcr.io` on private judge nodes before pulling.
- Do not use the `process` sandbox backend outside development, tests, and local real-code smoke.
- Do not reuse the local fake language seed as production language data.

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
internal/judgecore      Judge core pipeline, checker, sandbox adapters
internal/language       Language directory contract: profiles and the catalog
internal/contest        ACM contests, registrations, scoreboards
internal/postgres       SQL queries and generated sqlc code
internal/queue          Redis Stream task queue
internal/storage        S3-compatible object storage
internal/observability  Logging, health checks, Prometheus metrics, and tracing setup
```

## Roadmap

- Production dashboard and alert threshold tuning after trial traffic baselines are known.
- Reusable Grafana JSON after the dashboard query set stabilizes.

## License

Sundial Online Judge is released under the [MIT License](LICENSE).

## Links

- Backend (this repository): <https://github.com/sparklyi/SOJ>
- Frontend: <https://github.com/sparklyi/SOJ-web>
- Architecture: [docs/v2-architecture.md](docs/v2-architecture.md)
- API guide: [docs/v2-api-guide.md](docs/v2-api-guide.md)
- Worker operations: [docs/v2-worker.md](docs/v2-worker.md)
- Deployment: [docs/v2-deploy.md](docs/v2-deploy.md)
- Judge runtime readiness: [docs/judge-runtime-readiness.md](docs/judge-runtime-readiness.md)
- Contact: WeChat `sparkyi1026` · Email `sparkyi@foxmail.com`
