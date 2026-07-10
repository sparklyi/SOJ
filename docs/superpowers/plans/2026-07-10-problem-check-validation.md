# Problem Check Validation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a synchronous problem data validation MVP for the current testcase archive.

**Architecture:** Keep validation in `internal/problem` behind the existing service/repository/handler boundaries. Reuse existing `problem_check_runs` and `problem_check_findings` tables; do not add migrations for this slice. Return persisted run + findings through REST endpoints and OpenAPI.

**Tech Stack:** Go 1.25, Gin, PostgreSQL/sqlc, existing object storage abstraction, OpenAPI YAML.

**Status:** Completed on 2026-07-10.

---

## Scope

- Add `POST /api/v1/problems/{id}/checks` to run validation on the current ready testcase set.
- Add `GET /api/v1/problems/{id}/checks/{check_id}` to fetch a run and findings.
- Validate storage readability, zip parseability, input/output pairing, expected case count, empty archives, and statement sample JSON consistency.
- Persist one completed or failed check run plus findings.
- Keep checks synchronous for MVP; no worker queue, no background retries, no new migration.

## Parallel Work Slices

### Task 1: Service Validation

**Files:**
- Modify: `internal/problem/service.go`
- Modify: `internal/problem/service_test.go`

- [x] Add check run, finding, summary response types.
- [x] Add service method `RunProblemCheck(ctx, actor, problemID)` with owner/admin authorization.
- [x] Load current problem, current statement, current ready testcase set, and object storage bytes.
- [x] Produce findings for unreadable storage, invalid zip, missing pairs, case count mismatch, empty archive, and invalid statement samples.
- [x] Persist run and findings through repository methods.
- [x] Add focused service tests that first fail, then pass.

### Task 2: Repository Wiring

**Files:**
- Modify: `internal/problem/repository.go`
- Modify: `internal/problem/service_test.go`
- Regenerate/check: `internal/postgres/db/problems.sql.go`, `internal/postgres/db/querier.go` only if sqlc output requires it.

- [x] Extend `Repository` with create/get/list methods for check runs and findings.
- [x] Map DB rows to domain structs, including JSON summary/details and nullable fields.
- [x] Implement fake repository support for service tests.
- [x] Verify existing sqlc queries are sufficient before changing SQL.

### Task 3: HTTP And Contract

**Files:**
- Modify: `internal/problem/handler.go`
- Modify: `internal/problem/routes.go`
- Modify: `api/openapi.yaml`
- Modify: `docs/v2-api-guide.md`
- Optional test: add/extend handler or service tests if an existing HTTP test pattern is present.

- [x] Add route handlers for run and get check endpoints.
- [x] Parse `check_id`, return envelopes, and preserve existing error handling.
- [x] Add OpenAPI paths and schemas for `ProblemCheckRun`, `ProblemCheckFinding`, and envelope.
- [x] Document the endpoints in the API guide.

## Verification

- [x] `go test ./internal/problem`
- [x] `go test ./internal/postgres/db`
- [x] `go test ./...`
- [x] `go vet ./...`
- [x] `git diff --check`

## PR

- Branch: `feat/problem-check-validation`
- Commit message target: `feat(problem): expose testcase validation checks`
- PR base: `main`
