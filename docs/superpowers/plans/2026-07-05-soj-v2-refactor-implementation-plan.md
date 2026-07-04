# SOJ v2 Refactor Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the SOJ v2 backend as a modular Go system with Gin transport, PostgreSQL/sqlc persistence, Redis Stream judge tasks, S3-compatible storage, JudgeEngine abstraction, OpenAPI docs, and Docker Compose deployment.

**Architecture:** Keep v2 in the current repository while old code remains available for reference. Use `cmd/` entrypoints and focused `internal/` modules; Gin stays in `internal/httpapi`, business code receives `context.Context` plus explicit `auth.Actor`. Build foundations first, then split user/problem/submission/contest/deploy work across independent agents.

**Tech Stack:** Go 1.26.x, Gin, PostgreSQL, pgx, sqlc, Redis Stream, MinIO/S3, OpenAPI 3.x, Docker Compose.

---

## Chunk 1: Parallel Work Packages

### Dependency Map

Start with **WP0 Foundation** and **WP1 Database/API Contract**. WP0 owns shared Go interfaces and module registration conventions; WP1 owns shared schema, sqlc package conventions, and OpenAPI merge rules. After those compile, **WP2 Auth/User**, **WP3 Problem/Storage**, and **WP4 Judge Queue/Submission** can run in parallel. **WP5 Contest** depends on WP2, WP3, and WP4. **WP6 Deployment/Docs** can start after WP0 and finish last.

```text
WP0 Foundation
  -> WP1 Database/API Contract
      -> WP2 Auth/User
      -> WP3 Problem/Storage
      -> WP4 Judge Queue/Submission
          -> WP5 Contest
  -> WP6 Deployment/Docs
```

### WP0: v2 Foundation And Runtime

**Owner:** Foundation agent

**Files:**
- Create: `cmd/soj-api/main.go`
- Create: `cmd/soj-worker/main.go`
- Create: `cmd/soj-migrate/main.go`
- Create: `internal/app/api.go`
- Create: `internal/app/worker.go`
- Create: `internal/app/migrate.go`
- Create: `internal/config/config.go`
- Create: `internal/observability/logger.go`
- Create: `internal/observability/health.go`
- Create: `internal/httpapi/router.go`
- Create: `internal/httpapi/response.go`
- Create: `internal/httpapi/middleware.go`
- Create: `internal/httpapi/module.go`
- Create: `internal/auth/actor.go`
- Create: `internal/storage/storage.go`
- Create: `internal/queue/queue.go`
- Create: `internal/judge/judge.go`
- Create: `internal/problem/contracts.go`

- [ ] Build minimal entrypoints for API, worker, and migrate commands.
- [ ] Add shared contracts only: `auth.Actor`, `storage.ObjectStorage`, `queue.TaskQueue`, `judge.JudgeEngine`, `problem.Reader`, `problem.TestcaseResolver`, and `httpapi.Module`.
- [ ] Define route registration so feature agents add their own `RegisterRoutes(router *gin.RouterGroup, deps ...)` functions instead of editing central router logic.
- [ ] Implement typed config with env overrides and no dependency on committed secrets.
- [ ] Implement structured logging, request id middleware, API `/healthz` and `/readyz`, and worker health/readiness endpoint.
- [ ] Wire a minimal Gin router that returns standard response envelopes.
- [ ] Verify with `go test ./...` and `go run ./cmd/soj-api --help`.
- [ ] Commit: `feat: add v2 runtime foundation`.

### WP1: PostgreSQL Schema, sqlc, And OpenAPI Baseline

**Owner:** Data/API contract agent

**Files:**
- Create: `internal/migrations/000001_init.up.sql`
- Create: `internal/postgres/pool.go`
- Create: `internal/postgres/tx.go`
- Create: `internal/postgres/sqlc.yaml`
- Create: `internal/postgres/queries/users.sql`
- Create: `internal/postgres/queries/problems.sql`
- Create: `internal/postgres/queries/submissions.sql`
- Create: `internal/postgres/queries/contests.sql`
- Create: `internal/postgres/queries/languages.sql`
- Create: `api/openapi.yaml`
- Create: `docs/v2-api-guide.md`

- [ ] Translate the approved spec schema into the initial migration.
- [ ] Add sqlc config and first query groups for users, problems, submissions, runs, contests, and languages.
- [ ] Define query ownership: WP2 owns `users.sql`, WP3 owns `problems.sql`, WP4 owns `submissions.sql` and `languages.sql`, WP5 owns `contests.sql`.
- [ ] Define shared pagination, error, and response schemas in `api/openapi.yaml`.
- [ ] Add endpoint skeletons matching the spec's endpoint contract matrix.
- [ ] Define OpenAPI ownership: WP1 owns shared schemas, WP2 auth/admin-user paths, WP3 problem paths, WP4 submission/run/admin-language paths, WP5 contest paths.
- [ ] Verify migration on an empty PostgreSQL container using `SOJ_DATABASE_DSN=postgres://soj:soj@localhost:5432/soj?sslmode=disable go run ./cmd/soj-migrate up`.
- [ ] Run `sqlc generate` and `go test ./...`.
- [ ] Commit: `feat: add v2 schema and api contract`.

### WP2: Auth And User Module

**Owner:** Auth/user agent

**Depends On:** WP0, WP1

**Files:**
- Create: `internal/auth/jwt.go`
- Create: `internal/auth/password.go`
- Create: `internal/auth/refresh_tokens.go`
- Create: `internal/user/service.go`
- Create: `internal/user/handler.go`
- Create: `internal/user/repository.go`
- Create: `internal/user/routes.go`
- Modify: `internal/postgres/queries/users.sql`
- Modify: `api/openapi.yaml`

- [ ] Implement password hashing, JWT access tokens, refresh token hash storage, and logout revocation.
- [ ] Implement `Register`, `Login`, `Refresh`, `Logout`, `GET /me`, `GET /admin/users`, and `PATCH /admin/users/{id}`.
- [ ] Keep Gin only in handlers; service methods use `context.Context` and `auth.Actor`.
- [ ] Register routes through the module registration convention from WP0.
- [ ] Add unit tests for token lifecycle, refresh revocation, and role parsing.
- [ ] Add handler tests for 400/401/409/success cases.
- [ ] Run `go test ./internal/auth ./internal/user ./internal/httpapi`.
- [ ] Commit: `feat: add auth and user module`.

### WP3: Problem, Statement, Testcase, And Storage Module

**Owner:** Problem/storage agent

**Depends On:** WP0 shared contracts, WP1 schema/API baseline

**Files:**
- Create: `internal/storage/s3.go`
- Create: `internal/problem/service.go`
- Create: `internal/problem/handler.go`
- Create: `internal/problem/repository.go`
- Create: `internal/problem/testcase.go`
- Create: `internal/problem/routes.go`
- Modify: `internal/postgres/queries/problems.sql`
- Modify: `api/openapi.yaml`

- [ ] Implement `ObjectStorage` and MinIO/S3 adapter.
- [ ] Implement problem CRUD, current statement versioning, tag assignment, and problem stats query.
- [ ] Implement synchronous testcase archive validation, object write, row-lock version allocation, and current switch.
- [ ] Enforce publish rule: current statement plus current ready testcase set required.
- [ ] Add tests for owner/admin authorization, statement current switch, testcase validation failure, and concurrent upload serialization.
- [ ] Run `go test ./internal/storage ./internal/problem`.
- [ ] Commit: `feat: add problem and testcase module`.

### WP4: Judge Queue, Submission, Run, And Worker Module

**Owner:** Submission/worker agent

**Depends On:** WP0 shared contracts, WP1 schema/API baseline

**Files:**
- Create: `internal/queue/redis_stream.go`
- Create: `internal/judge/judge0.go`
- Create: `internal/judge/fake.go`
- Create: `internal/submission/service.go`
- Create: `internal/submission/handler.go`
- Create: `internal/submission/repository.go`
- Create: `internal/submission/worker.go`
- Create: `internal/submission/reconciler.go`
- Create: `internal/submission/routes.go`
- Modify: `cmd/soj-worker/main.go`
- Modify: `internal/postgres/queries/submissions.sql`
- Modify: `internal/postgres/queries/languages.sql`
- Modify: `api/openapi.yaml`

- [ ] Implement `TaskQueue` with Redis Stream publish, consume, ack, claim stale, and dead-letter.
- [ ] Implement `JudgeEngine` interface, fake adapter for tests, and Judge0 adapter boundary.
- [ ] Consume the `problem.Reader` and `problem.TestcaseResolver` contracts from WP0; use fakes until WP3 implementation is merged.
- [ ] Implement formal submission creation: store source artifact, create queued submission, create pending judge task.
- [ ] Implement worker loop, dispatching, running recovery, retry backoff, dead-letter, and idempotent terminal updates.
- [ ] Implement self-run as custom stdin only, with short wait and stale run reconciliation.
- [ ] Implement admin language list, sync, and update endpoints against the JudgeEngine language sync boundary.
- [ ] Register routes through the module registration convention from WP0.
- [ ] Add tests for duplicate messages, stale claim, retry, dead-letter, stale run marking, and terminal-state idempotency.
- [ ] Run `go test ./internal/queue ./internal/judge ./internal/submission`.
- [ ] Commit: `feat: add judge queue and submission worker`.

### WP5: ACM Contest Module

**Owner:** Contest agent

**Depends On:** WP2, WP3, WP4

**Files:**
- Create: `internal/contest/service.go`
- Create: `internal/contest/handler.go`
- Create: `internal/contest/repository.go`
- Create: `internal/contest/scoreboard.go`
- Create: `internal/contest/routes.go`
- Modify: `internal/submission/service.go`
- Modify: `internal/postgres/queries/contests.sql`
- Modify: `api/openapi.yaml`

- [ ] Implement contest CRUD, ordered problem aliases, private invite hash, and registration.
- [ ] Enforce contest submit rules: published contest, registration required, time window valid.
- [ ] Update contest problem results from terminal submission results in an idempotent transaction.
- [ ] Implement live/frozen/final scoreboard row and cell schema exactly as specified.
- [ ] Register routes through the module registration convention from WP0.
- [ ] Add tests for ACM penalty, tie rank, frozen hidden attempts, final snapshot fallback, and owner/admin live view.
- [ ] Run `go test ./internal/contest ./internal/submission`.
- [ ] Commit: `feat: add acm contest module`.

### WP6: Docker Compose, Docs, And Cutover

**Owner:** Deployment/docs agent

**Depends On:** WP0, partial WP1; finish after WP5

**Files:**
- Create: `deploy/docker-compose.yaml`
- Create: `deploy/config.example.yaml`
- Create: `Dockerfile.v2`
- Create: `docs/v2-architecture.md`
- Create: `docs/v2-deploy.md`
- Create: `docs/v2-worker.md`
- Modify: `README.md`
- Later modify/delete: root `docker-compose.yaml`, old entrypoint references

- [ ] Add Docker Compose for API, worker, migrate, PostgreSQL, Redis, MinIO, Judge0 server/worker/db/redis.
- [ ] Add non-secret example config and document required env vars.
- [ ] Add Go 1.26.x Dockerfile for v2 commands.
- [ ] Document architecture, deployment, worker recovery, dead-letter operations, and API guide.
- [ ] Verify `docker compose -f deploy/docker-compose.yaml config`.
- [ ] After WP5 passes, verify `docker compose -f deploy/docker-compose.yaml up` reaches healthy API/worker.
- [ ] Commit: `docs: add v2 deployment and operations guide`.

### Integration Gate

**Owner:** Integration agent or final coordinator

**Files:**
- Modify as needed across v2 modules only.

- [ ] Run `go test ./...`.
- [ ] Run `go vet ./...`.
- [ ] Run migration against an empty PostgreSQL database.
- [ ] Run `docker compose -f deploy/docker-compose.yaml config`.
- [ ] Smoke test: register/login, create problem, upload statement/testcase, submit solution, worker reaches terminal state, create contest, register, submit contest solution, fetch scoreboard.
- [ ] Update docs for any intentionally changed endpoint or operational behavior.
- [ ] Commit final integration fixes: `chore: integrate soj v2 backend`.

### Parallelization Notes

- WP0 and WP1 should be short-lived and merged first.
- WP2, WP3, and WP4 can run in parallel after shared interfaces are stable.
- WP5 should start with repository/service skeletons while WP4 is finishing, but final scoring integration waits for terminal submission events.
- WP6 can run alongside all work, but final health checks wait for WP5.
- Each worker should avoid editing old v1 code unless assigned to the final cutover.
