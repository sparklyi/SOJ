# SOJ v2 Architecture

SOJ v2 targets Go 1.24 and splits the backend into three commands:

- `soj-api`: Gin HTTP transport and REST API.
- `soj-worker`: judge task dispatcher, Redis Stream consumer, and reconciliation loops.
- `soj-migrate`: versioned PostgreSQL migration runner.

Gin is kept at the transport boundary. Business services receive `context.Context` plus explicit `auth.Actor`; repositories use PostgreSQL through sqlc/pgx. Redis Stream coordinates judge task delivery, but PostgreSQL remains the source of truth for submissions, runs, and judge tasks.

## Runtime Dependencies

- PostgreSQL: primary relational store.
- Redis: judge task stream and consumer group coordination.
- MinIO/S3: source files, testcase archives, and future large artifacts.
- JudgeEngine: pluggable judge backend. Local Docker uses `fake://accepted`; production can switch to Judge0 behind the same interface.

## Module Boundaries

- `internal/auth` owns actors, token logic, password hashing, and refresh token policy.
- `internal/user` owns account and admin user use cases.
- `internal/problem` owns problem metadata, statements, testcase sets, and publish rules.
- `internal/submission` owns submissions, self-runs, judge tasks, and worker orchestration.
- `internal/contest` owns ACM contests and scoreboards.
- `internal/httpapi` owns request binding, response envelopes, and Gin route registration conventions.

## Deployment Boundary

The supported local deployment is `deploy/docker-compose.yaml`. It runs migration and seed as one-shot jobs before API/worker startup, then validates the core workflow through `deploy/smoke.sh`.

## Legacy Boundary

The v1 implementation is archived on the `archive/v1` branch. Main keeps only the v2 backend path and the old root Dockerfile, root Compose file, MySQL/Gorm/Mongo/RabbitMQ modules, and v1 HTTP wiring are removed from the active tree.
