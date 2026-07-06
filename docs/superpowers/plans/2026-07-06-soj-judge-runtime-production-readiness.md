# SOJ Judge Runtime Production Readiness Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the judge runtime ready for serious trial deployment with dependency readiness checks, dead-task recovery, runtime metrics, and documented local validation evidence.

**Architecture:** Keep the async judge architecture unchanged. Add small readiness probe adapters at the process boundary, a worker-owned recovery command that treats PostgreSQL as the source of truth, and metrics/reporting around those operational paths.

**Tech Stack:** Go 1.24, Gin health routes, pgx/sqlc, go-redis, MinIO client, Prometheus client, Docker Compose, gVisor/runsc tooling.

---

## File Structure

- Modify `internal/observability/health.go` and tests for named readiness check results and metrics recording.
- Modify `internal/observability/metrics.go` and tests for readiness, recovery, and reconciler counters/histograms.
- Modify `internal/queue/queue.go`, `internal/queue/redis_stream.go`, and tests to expose lightweight Redis stream readiness.
- Modify `internal/storage/storage.go`, `internal/storage/s3.go`, and tests to expose object storage readiness.
- Modify `internal/app/worker.go` to assemble worker readiness and route `recover-dead-task`.
- Modify `internal/app/judge_agent.go` to assemble judge-agent readiness with Redis, storage, and sandbox probe checks.
- Modify `internal/submission/repository.go`, `internal/postgres/queries/submissions.sql`, and generated sqlc output for dead task recovery.
- Modify `internal/submission/reconciler.go` and tests to record reconciliation metrics.
- Add `docs/judge-runtime-readiness.md`.
- Update `README.md`, `README.zh-CN.md`, and `docs/v2-deploy.md` links/status.

## Chunk 1: Readiness And Metrics Foundations

**Files:**
- Modify: `internal/observability/health.go`
- Modify: `internal/observability/health_test.go`
- Modify: `internal/observability/metrics.go`
- Modify: `internal/observability/metrics_test.go`
- Modify: `internal/queue/queue.go`
- Modify: `internal/queue/redis_stream.go`
- Modify: `internal/queue/redis_stream_test.go`
- Modify: `internal/storage/storage.go`
- Modify: `internal/storage/s3.go`

- [ ] Add failing tests for readiness checks recording dependency success/failure without leaking sensitive error details.
- [ ] Add failing tests for queue readiness checking stream/group visibility.
- [ ] Add failing tests for metrics exposure: readiness checks, recovery actions, reconciler actions.
- [ ] Implement minimal readiness result recording and metrics methods.
- [ ] Implement queue/storage readiness methods.
- [ ] Run `go test ./internal/observability ./internal/queue ./internal/storage`.

## Chunk 2: Worker And Judge-Agent Readiness Wiring

**Files:**
- Modify: `internal/app/worker.go`
- Modify: `internal/app/judge_agent.go`
- Add or modify app tests if existing seams are sufficient.
- Modify: `docs/v2-deploy.md`

- [ ] Add failing tests or small seam tests proving worker readiness includes PostgreSQL, request Redis stream, result Redis stream, and object storage.
- [ ] Add failing tests or seam tests proving judge-agent readiness includes Redis streams, object storage, and sandbox probe.
- [ ] Wire worker `ReadyCheck` into `httpapi.NewRouter`.
- [ ] Replace judge-agent single Redis ping readiness with composed readiness.
- [ ] Document current readiness checks in deploy docs.
- [ ] Run `go test ./internal/app ./internal/observability ./internal/queue ./internal/storage`.

## Chunk 3: Dead Task Recovery

**Files:**
- Modify: `internal/postgres/queries/submissions.sql`
- Regenerate: `internal/postgres/db/submissions.sql.go`, `internal/postgres/db/querier.go`
- Modify: `internal/submission/repository.go`
- Modify: `internal/submission/reconciler.go`
- Modify or add: `internal/submission/*_test.go`
- Modify: `internal/app/worker.go`

- [ ] Add failing repository/memory tests for recovering a `dead` judge task back to `pending`.
- [ ] Add SQL query `RecoverDeadJudgeTask`.
- [ ] Regenerate sqlc output with the project command or available `sqlc`.
- [ ] Implement repository method and memory test support.
- [ ] Add `soj-worker recover-dead-task -task-id <id> [-reason <text>]`.
- [ ] Add metrics for recovery success, not-found/not-dead, and errors.
- [ ] Run `go test ./internal/submission ./internal/app`.

## Chunk 4: Validation Docs

**Files:**
- Add: `docs/judge-runtime-readiness.md`
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `docs/v2-deploy.md`

- [ ] Run local verification commands and capture exact results.
- [ ] Write readiness and recovery operations doc.
- [ ] Add README/deploy doc links.

## Final Verification

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `docker compose -f deploy/docker-compose.yaml config`
- [ ] `make smoke-real-docker`
- [ ] Go feature-change review of modified Go code before commit/PR
