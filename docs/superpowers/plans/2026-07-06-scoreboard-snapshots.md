# Scoreboard Snapshots Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically create frozen and final contest scoreboard snapshots from the worker reconciliation loop.

**Architecture:** Keep scoreboard rules inside `internal/contest`. Add repository methods for due-contest discovery and snapshot creation, expose a service method that generates missing frozen/final snapshots idempotently, and have the worker reconciliation loop call that method after existing submission reconciliation work.

**Tech Stack:** Go 1.24, pgx/sqlc generated query wrappers, existing contest service tests, worker app loop.

---

## File Structure

- Modify `internal/contest/service.go` for snapshot generation input/output types.
- Modify `internal/contest/repository.go` for due-contest discovery and snapshot persistence.
- Modify `internal/contest/scoreboard.go` for a public service method that creates snapshots without auth checks.
- Modify `internal/contest/service_test.go` and `internal/contest/memory_repository_test.go` for TDD coverage.
- Modify `internal/postgres/queries/contests.sql`, `internal/postgres/db/contests.sql.go`, and `internal/postgres/db/querier.go` because `sqlc` is unavailable locally.
- Modify `internal/app/worker.go` to call snapshot generation from the reconciler loop.
- Update `README.md`, `README.zh-CN.md`, and `docs/v2-worker.md` to remove the stale follow-up wording.

## Chunk 1: Contest Snapshot Generation

- [x] Write failing tests for generating frozen and final snapshots exactly once.
- [x] Run `go test ./internal/contest -run Snapshot` and verify failure.
- [x] Add repository methods and service implementation.
- [x] Run `go test ./internal/contest`.

## Chunk 2: PostgreSQL And Worker Wiring

- [x] Add SQL query for contests missing due snapshots.
- [x] Manually update generated sqlc files to match query shape.
- [x] Wire `contest.Service.GenerateDueScoreSnapshots` into worker reconciliation.
- [x] Run `go test ./internal/contest ./internal/app`.

## Chunk 3: Documentation And Verification

- [x] Update docs to describe automatic snapshot generation as current behavior.
- [x] Run `gofmt` on changed Go files.
- [x] Run `go test ./...`.
- [x] Run `go vet ./...`.
