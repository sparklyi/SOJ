# SOJ Judge Core MVP Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development to implement this plan. Keep modules independent, commit by milestone, and avoid expanding this plan into a large design document.

**Goal:** Build the first production-shaped Judge Core MVP: async judge events, independent judge agent, real Go/C++17 execution pipeline, sandbox boundary, metrics, and Docker full-flow validation.

**Architecture:** Production judging is asynchronous. Dispatcher publishes `judge.request.v1`, `soj-judge-agent` consumes and publishes `judge.result.v1`, and SOJ result consumer persists terminal results transactionally. Judge Core is modular: language profiles, sandbox, runner, checker, testcase cache, artifact, manifest, and event adapters stay behind interfaces.

**Tech Stack:** Go, Gin only at process edges, PostgreSQL/sqlc, Redis Stream, MinIO artifact storage, Docker Compose, Prometheus metrics, `isolate` sandbox for production path with dev-only process fallback.

---

## Phase Split

### Phase 0: Async Event Contract And Worker Split

**Scope**
- Define compact `judge.request.v1`, `judge.progress.v1`, `judge.result.v1`, `judge.dead_letter.v1` DTOs.
- Add canonical verdict mapper: `time_limit`, `memory_limit`, `output_limit`, etc.
- Split current synchronous worker path into dispatcher, fake async agent, and result consumer.
- Add request outbox and result inbox semantics with idempotent event ids.
- Keep current `JudgeEngine` only as test/dev compatibility.

**Files**
- `internal/judge/events`
- `internal/judge/protocol.go`
- `internal/submission/worker.go`
- `internal/submission/repository.go`
- `internal/postgres/queries/submissions.sql`
- `internal/queue`

**Commit**
- `feat(judge): add async judge event pipeline`

**Verification**
- `go test ./internal/judge ./internal/submission ./internal/queue`

### Phase 1: Judge Agent Process

**Scope**
- Add `cmd/soj-judge-agent`.
- Implement Redis Stream consumer group handling, health endpoint, heartbeat, graceful shutdown.
- Enforce per-agent global slots and per-language slots.
- Implement fake runner path first so full async loop works before real sandbox.

**Files**
- `cmd/soj-judge-agent`
- `internal/app`
- `internal/judge/events`
- `internal/queue`
- `deploy/docker-compose.yaml`

**Commit**
- `feat(judge): add judge agent process`

**Verification**
- `go test ./cmd/soj-judge-agent ./internal/app ./internal/judge/...`

### Phase 2: Judge Core Pipeline

**Scope**
- Add `internal/judgecore` pipeline: prepare, compile, run, check, aggregate, manifest, cleanup.
- Add language registry with Go and C++17 profiles.
- Add built-in checkers: exact, trailing-whitespace tolerant, whitespace tolerant.
- Add testcase cache interface and artifact refs; do not inline large outputs in events.
- Implement dev-only process sandbox adapter for unit tests.

**Files**
- `internal/judgecore`
- `internal/judgecore/language`
- `internal/judgecore/sandbox`
- `internal/judgecore/runner`
- `internal/judgecore/checker`
- `internal/judgecore/testcase`
- `internal/judgecore/artifact`
- `internal/judgecore/manifest`

**Commit**
- `feat(judgecore): add core execution pipeline`

**Verification**
- `go test ./internal/judgecore/...`

### Phase 3: Sandbox And Safety

**Scope**
- Add `isolate` sandbox adapter behind the same sandbox interface.
- Production startup must reject unsafe `process` backend.
- Enforce CPU, wall, memory, output, temp disk, process, fd, and no-network limits.
- Add abuse tests for loop, memory, large output, abnormal exit, file/network access where local environment supports it.

**Files**
- `internal/judgecore/sandbox`
- `cmd/soj-judge-agent`
- `deploy/docker-compose.yaml`
- `docs/v2-deploy.md`

**Commit**
- `feat(judgecore): add sandbox resource isolation`

**Verification**
- `go test ./internal/judgecore/...`
- Docker agent starts with safe sandbox configuration.

### Phase 4: Docker Flow, Metrics, Docs

**Scope**
- Wire API, worker/dispatcher, judge-agent, Redis, PostgreSQL, MinIO, and Prometheus in Docker Compose.
- Update smoke test to prove submit -> dispatch -> agent -> result consumer -> safe API projection.
- Add queue, agent slot, sandbox phase duration, verdict, system error, and testcase cache metrics.
- Update README, API/deploy docs only where behavior changed.

**Files**
- `deploy/docker-compose.yaml`
- `deploy/smoke.sh`
- `README.md`
- `docs/v2-api-guide.md`
- `docs/v2-deploy.md`
- `internal/metrics` or existing metrics packages

**Commit**
- `docs(judge): document judge core docker flow`

**Verification**
- `go test ./...`
- `go vet ./...`
- `docker compose -f deploy/docker-compose.yaml down -v --remove-orphans`
- `docker compose -f deploy/docker-compose.yaml up --build -d`
- `./deploy/smoke.sh`

## Parallel Work Allocation

- Agent A: Phase 0 event contract, repository idempotency, worker split.
- Agent B: Phase 1 agent command, lifecycle, Redis consumer behavior.
- Agent C: Phase 2 judgecore pipeline, language profiles, checkers.
- Agent D: Phase 3 sandbox adapter and safety tests.
- Main coordinator: integrate results, resolve conflicts, commit milestones, run Docker validation.

## Done Criteria

- Production path is async and does not synchronously wait for Judge Core execution.
- Agent does not write business database state.
- Go and C++17 real submissions produce normalized terminal results.
- Attempt, case result, current projection, and contest projection stay transactionally consistent.
- Unsafe process backend cannot be selected in production.
- Local Docker full-flow smoke passes from clean volumes.

