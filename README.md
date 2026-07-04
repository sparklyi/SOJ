# SOJ

SOJ is an open-source online judge system. The backend has been cut over to the v2 architecture: a smaller, clearer Go service with PostgreSQL as the source of truth, Redis Stream for judge task delivery, S3-compatible object storage, and a pluggable `JudgeEngine`.

The old v1 code has been archived on the `archive/v1` branch. New backend development should target the v2 `cmd/`, `internal/`, `api/`, `docs/`, and `deploy/` paths.

## Current Status

The v2 backend currently includes:

- User registration, login, refresh tokens, and admin user management.
- Problem metadata, statements, tags, testcase archive upload, publish checks, and stats.
- Submissions, self-runs, judge tasks, Redis Stream worker, retry/dead-letter handling, and reconciliation loops.
- ACM contests, registration, contest submission policy, and live/frozen/final scoreboard responses.
- Versioned PostgreSQL migrations and an OpenAPI contract for frontend integration.
- Local Docker Compose deployment with PostgreSQL, Redis, MinIO, API, worker, migration, seed, and smoke test flow.

Known follow-up: frozen/final scoreboard snapshots are read when present and otherwise generated synchronously from current data. Automated snapshot generation is planned as a later worker responsibility.

## Quick Start

Requirements:

- Docker with Compose v2
- `curl`, `jq`, `zip`, and `shasum` for the smoke test

Start a clean local v2 stack:

```bash
docker compose -f deploy/docker-compose.yaml down -v --remove-orphans
docker compose -f deploy/docker-compose.yaml up --build -d
./deploy/smoke.sh
```

The API listens on `http://localhost:8080`; the worker health endpoint listens on `http://localhost:8081`.

Local Docker uses `SOJ_JUDGE_ENDPOINT=fake://accepted` and seeds one enabled fake language so the submit/worker flow can be tested without a privileged judge sandbox.

## Development

Run the main checks:

```bash
go test ./...
go vet ./...
docker compose -f deploy/docker-compose.yaml config
```

Run only the Docker smoke test against an already running stack:

```bash
./deploy/smoke.sh
```

The v2 runtime is built and verified with Go 1.24.

## Project Layout

```text
api/                    OpenAPI contract
cmd/soj-api             HTTP API entrypoint
cmd/soj-worker          Judge worker entrypoint
cmd/soj-migrate         PostgreSQL migration entrypoint
deploy/                 Docker Compose, env examples, smoke test
docs/                   v2 architecture, API guide, worker and deploy docs
internal/app            Runtime assembly for commands
internal/auth           Actor, JWT, password, token primitives
internal/user           Account and admin user use cases
internal/problem        Problems, statements, tags, testcase sets
internal/submission     Submissions, runs, judge tasks, worker logic
internal/contest        ACM contests, registrations, scoreboards
internal/postgres       SQL queries and generated sqlc code
internal/queue          Redis Stream task queue
internal/storage        S3-compatible object storage
```

## Architecture

Gin is kept at the transport boundary. Business services receive `context.Context` and explicit `auth.Actor` values. Repositories use PostgreSQL through `sqlc`/`pgx`.

Runtime dependencies:

- PostgreSQL: primary relational data store.
- Redis: judge task stream and consumer group coordination.
- MinIO/S3: source code, testcase archives, and future large artifacts.
- JudgeEngine: abstraction for fake local judge, Judge0, or a future runner.

PostgreSQL remains the source of truth for submissions, runs, judge tasks, and contest results. Redis Stream messages are delivery hints and workers must tolerate duplicate deliveries.

## API And Docs

- API contract: `api/openapi.yaml`
- API guide: `docs/v2-api-guide.md`
- Architecture: `docs/v2-architecture.md`
- Docker deployment: `docs/v2-deploy.md`
- Worker operations: `docs/v2-worker.md`
- Refactor design: `docs/superpowers/specs/2026-07-04-soj-v2-refactor-design.md`
- Implementation plan: `docs/superpowers/plans/2026-07-05-soj-v2-refactor-implementation-plan.md`

## Deployment Notes

The supported local deployment is `deploy/docker-compose.yaml`.

Production deployment should replace local defaults before exposure:

- Use a real `SOJ_JWT_SECRET`.
- Use production PostgreSQL, Redis, and object storage credentials.
- Replace `fake://accepted` with a real judge backend.
- Do not reuse the local fake language seed as production language data.

## Legacy Code

Historical v1 code, root-level Docker files, and earlier integrations are preserved on the `archive/v1` branch. The main branch now keeps only the v2 backend path.

## License

SOJ is released under the MIT License.

## Contact

- WeChat: `sparkyi1026`
- Email: `sparkyi@foxmail.com`
