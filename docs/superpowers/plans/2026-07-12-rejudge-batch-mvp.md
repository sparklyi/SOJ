# Rejudge Batch MVP Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow an authorized operator to rejudge a fixed set of terminal submissions for a problem or ended contest with durable progress, cancellation, attempt linkage, and scoreboard refresh.

**Architecture:** Add durable `rejudge_batch_items` so batch membership and per-submission state do not depend on live queries. Batch creation transactionally snapshots eligible submissions and resets each submission's existing one-to-one judge task; normal worker dispatch then creates a new attempt linked to the batch. Result persistence updates the item and batch counters idempotently, while ended-contest batches publish a new final scoreboard snapshot only after all runnable items finish.

**Tech Stack:** Go 1.25, Gin, PostgreSQL 16, pgx/sqlc, Redis Stream, existing worker/result-consumer pipeline, OpenAPI, Docker Compose smoke.

**Status:** Implemented on 2026-07-12. Because the project is not deployed, the item table was added directly to the initial migration. Final scoreboard refresh uses the existing worker snapshot candidate loop, which retries until a snapshot newer than the completed contest batch exists.

---

## Scope And Invariants

- Exactly one target is required: `problem_id` or `contest_id`.
- Contest batches are limited to ended contests in this MVP.
- Only terminal submissions are eligible.
- A submission may belong to only one active rejudge batch at a time.
- Batch membership is immutable after creation.
- Existing attempts and case results remain immutable history.
- The existing `judge_tasks` row is reset and reused because the schema enforces one task per submission.
- An undispatched item can be canceled. Already running attempts may finish; cancellation does not roll results back.
- Repeated result messages must not increment progress twice.
- A completed ended-contest batch creates a newer `final` scoreboard snapshot.

## File Structure

**Create:**

- `internal/submission/rejudge.go`: service types, authorization, target validation, creation/list/detail/cancel orchestration.
- `internal/submission/rejudge_test.go`: service behavior with a focused fake repository.
- `internal/submission/rejudge_handler_test.go`: HTTP and authorization coverage.

**Modify:**

- `internal/migrations/000001_init.up.sql`: durable batch items, active-item uniqueness and task reset support indexes. The project is not deployed, so the initial schema remains the single source of truth.
- `internal/postgres/queries/submissions.sql`: batch/item selection, task reset, item completion and progress queries.
- `internal/postgres/db/*.go`: regenerated sqlc output.
- `internal/postgres/db/submissions_sql_test.go`: schema and guarded transition assertions.
- `internal/submission/repository.go`: records, repository interface and transactional SQL implementation.
- `internal/submission/service.go`: expose rejudge service dependency without mixing batch logic into submission creation.
- `internal/submission/worker.go`: pass batch identity into new judge attempts.
- `internal/submission/async.go`: keep event behavior unchanged; verify attempt identity propagation.
- `internal/submission/handler.go`: rejudge endpoints.
- `internal/submission/routes.go`: owner/admin protected routes.
- `internal/submission/memory_repo_test.go`: satisfy repository interface and support shared tests.
- `internal/submission/async_test.go`: attempt linkage and duplicate result tests.
- `internal/submission/contest_hook_test.go`: ended-contest snapshot refresh behavior.
- `internal/contest/service.go`: explicit final snapshot rebuild entry point for completed batches.
- `internal/contest/service_test.go`: final snapshot replacement tests.
- `internal/app/api.go`: wire rejudge service and contest completion hook.
- `internal/app/worker.go`: run batch reconciliation if completion is not fully result-driven.
- `internal/observability/metrics.go`: rejudge batch/item counters.
- `api/openapi.yaml`: contracts and endpoints.
- `docs/v2-api-guide.md`: operator workflow and cancellation semantics.
- `docs/v2-worker.md`: batch recovery and troubleshooting.
- `deploy/smoke.sh`: ended-contest rejudge scenario.

## Chunk 1: Durable Data Model

### Task 1: Add Rejudge Batch Items

**Files:**
- Modify: `internal/migrations/000001_init.up.sql`
- Modify: `internal/postgres/db/submissions_sql_test.go`

- [ ] Add a failing schema test that requires `rejudge_batch_items` with `batch_id`, `submission_id`, `task_id`, `attempt_id`, `status`, `error_message`, timestamps and uniqueness on `(batch_id, submission_id)`.
- [ ] Add a failing schema test for a partial unique index preventing more than one `queued` or `running` item per submission.
- [ ] Run `go test ./internal/postgres/db -run Rejudge -count=1` and verify failure.
- [ ] Add `rejudge_batch_items` directly to `000001_init.up.sql` with item statuses `queued`, `running`, `completed`, `failed`, `canceled`.
- [ ] Add indexes for batch status scans, submission active membership and task/attempt lookup.
- [ ] Keep `rejudge_batches` as the aggregate root; do not duplicate reason or target fields on items.
- [ ] Run the focused database tests and `git diff --check`.
- [ ] Commit: `feat(rejudge): add durable batch items`

### Task 2: Add Selection And Transition Queries

**Files:**
- Modify: `internal/postgres/queries/submissions.sql`
- Regenerate: `internal/postgres/db/submissions.sql.go`
- Regenerate: `internal/postgres/db/querier.go`
- Modify: `internal/postgres/db/submissions_sql_test.go`

- [ ] Add failing SQL text tests requiring terminal-only selection by problem and by contest.
- [ ] Add `ListEligibleSubmissionsForProblemRejudge` and `ListEligibleSubmissionsForContestRejudge` with stable `ORDER BY id` and no arbitrary dynamic SQL filters.
- [ ] Add `CreateRejudgeBatchItem` with conflict protection.
- [ ] Add `ListRejudgeBatchItems`, `GetRejudgeBatchItemByTaskID`, and `GetRejudgeBatchItemByAttemptID`.
- [ ] Add `PrepareJudgeTaskForRejudge` that resets only an existing `done` or `dead` task to `pending`, clears stream/error state, resets retry count and sets `next_run_at`.
- [ ] Add `PrepareSubmissionForRejudge` that changes a terminal submission to `queued`, clears terminal timing/resource fields, and leaves attempt history intact.
- [ ] Ensure submission views do not expose the previous `submission_results` projection while the submission is queued or running for rejudge.
- [ ] Add guarded item transitions: queued to running/canceled, running to completed/failed.
- [ ] Add a single aggregate query that derives counts from items and transitions the batch to completed only when no queued/running items remain.
- [ ] Regenerate sqlc with the repository's established command.
- [ ] Run `go test ./internal/postgres/db -count=1`.
- [ ] Commit: `feat(rejudge): add batch lifecycle queries`

## Chunk 2: Repository Transaction Boundary

### Task 3: Model Batch Records And Repository Contract

**Files:**
- Modify: `internal/submission/repository.go`
- Modify: `internal/submission/memory_repo_test.go`

- [ ] Add records for `RejudgeBatchRecord`, `RejudgeBatchItemRecord`, target input and list filter.
- [ ] Add repository methods for create/list/get/cancel plus item lookup and completion.
- [ ] Keep the public repository method coarse-grained: `CreateRejudgeBatchWithItems` must own the transaction that snapshots submissions, creates items, resets tasks and queues submissions.
- [ ] Do not expose a service sequence that can commit a batch without all items.
- [ ] Add fake repository support without sharing production SQL assumptions.
- [ ] Run `go test ./internal/submission -run Rejudge -count=1` and confirm compile failures are resolved only after the interface is complete.
- [ ] Commit: `refactor(rejudge): define repository boundary`

### Task 4: Implement Transactional Batch Creation

**Files:**
- Modify: `internal/submission/repository.go`
- Test: `internal/submission/rejudge_test.go`

- [ ] Write a failing test proving an empty eligible set returns a validation error and creates no batch.
- [ ] Write a failing test proving any task reset failure rolls back the batch and every item.
- [ ] Write a failing test proving duplicate active membership is rejected without altering the existing task.
- [ ] Implement one PostgreSQL transaction: select eligible submissions, create batch, create each item, reset task, queue submission, and store task id on the item.
- [ ] Lock selected submissions and judge tasks in stable id order to reduce deadlock risk.
- [ ] Set `total_count` from the inserted item count, never from a separate unprotected count query.
- [ ] Run focused repository/service tests.
- [ ] Commit: `feat(rejudge): create batches atomically`

## Chunk 3: Rejudge Service And Authorization

### Task 5: Implement Rejudge Service

**Files:**
- Create: `internal/submission/rejudge.go`
- Create: `internal/submission/rejudge_test.go`

- [ ] Write failing tests for anonymous, unrelated user, problem owner, contest owner, admin and root actors.
- [ ] Write failing tests requiring a non-empty bounded reason.
- [ ] Write failing tests for both targets set, neither target set, non-ended contest and missing target.
- [ ] Implement `CreateRejudgeBatch`, `GetRejudgeBatch`, `ListRejudgeBatches`, and `CancelRejudgeBatch`.
- [ ] Authorize problem batches through the problem ownership boundary and contest batches through the contest ownership boundary; admin/root may operate both.
- [ ] Limit page size to 100 and use stable status/target filters only.
- [ ] Return batch items on detail but not on list.
- [ ] Cancellation marks queued items canceled and leaves running items to finish; document partial-result semantics.
- [ ] Run `go test ./internal/submission -run Rejudge -count=1`.
- [ ] Commit: `feat(rejudge): add batch service`

### Task 6: Link Worker Attempts To Batch Items

**Files:**
- Modify: `internal/submission/worker.go`
- Modify: `internal/submission/repository.go`
- Modify: `internal/submission/async_test.go`

- [ ] Write a failing test proving dispatch of a rejudge task creates a higher attempt number with the expected `rejudge_batch_id`.
- [ ] Add `RejudgeBatchID` to `EnsureJudgeAttemptInput` and persist it through `CreateJudgeAttempt`.
- [ ] Resolve the queued item by task id before creating the attempt.
- [ ] Atomically attach `attempt_id` and move the item from queued to running; tolerate replay of the same task/attempt.
- [ ] Keep ordinary submissions unchanged when no active item exists.
- [ ] Verify a duplicate Redis request produces the same attempt rather than a second item transition.
- [ ] Run `go test ./internal/submission -run 'Rejudge|Async' -count=1`.
- [ ] Commit: `feat(rejudge): link judge attempts to batches`

## Chunk 4: Result Progress And Scoreboard

### Task 7: Update Batch Progress From Results

**Files:**
- Modify: `internal/submission/repository.go`
- Modify: `internal/submission/async_test.go`

- [ ] Write a failing test proving a successful terminal result completes exactly one item and increments the batch once.
- [ ] Write a failing duplicate-result test proving counters do not increment twice.
- [ ] Write a failing system-error test proving the item becomes failed while the submission result is still persisted.
- [ ] Update the result transaction to find the batch item by attempt id, apply a guarded item transition, and refresh aggregate counts.
- [ ] Treat judge verdicts as completed items; reserve failed item status for infrastructure failures that prevent a trustworthy terminal result.
- [ ] Complete the batch when no queued/running items remain and at least one item completed or failed.
- [ ] Preserve canceled batch status when late running items finish; update counts without changing it back to completed.
- [ ] Run focused async tests and full submission tests.
- [ ] Commit: `feat(rejudge): track result progress idempotently`

### Task 8: Refresh Ended Contest Final Snapshot

**Files:**
- Modify: `internal/contest/service.go`
- Modify: `internal/contest/service_test.go`
- Modify: `internal/submission/contest_hook_test.go`
- Modify: `internal/app/api.go`

- [ ] Write a failing test proving a completed contest batch creates a newer final snapshot from current contest projections.
- [ ] Write a failing test proving an incomplete or canceled batch does not publish a new final snapshot.
- [ ] Add an explicit `RebuildFinalScoreSnapshot` contest service method restricted to ended contests.
- [ ] Invoke the hook only on the transaction that transitions a batch to completed; make hook replay safe by recording completion and comparing snapshot generation time/batch marker.
- [ ] Add a worker reconciler that finds completed contest batches without a published snapshot and retries safely; do not rely on a one-shot cross-service callback.
- [ ] Do not delete prior snapshots; the newest snapshot becomes authoritative.
- [ ] Run contest and submission hook tests.
- [ ] Commit: `feat(rejudge): refresh final contest scoreboard`

## Chunk 5: HTTP Contract And Observability

### Task 9: Expose Rejudge APIs

**Files:**
- Modify: `internal/submission/handler.go`
- Modify: `internal/submission/routes.go`
- Create: `internal/submission/rejudge_handler_test.go`
- Modify: `api/openapi.yaml`
- Modify: `docs/v2-api-guide.md`

- [ ] Add failing handler tests for create, list, detail, cancel, malformed target, forbidden actor and conflict.
- [ ] Add endpoints:
  - `POST /api/v1/rejudge-batches`
  - `GET /api/v1/rejudge-batches`
  - `GET /api/v1/rejudge-batches/{id}`
  - `POST /api/v1/rejudge-batches/{id}/cancel`
- [ ] Use stable error codes including `rejudge.target_invalid`, `rejudge.no_submissions`, `rejudge.active_conflict`, `rejudge.contest_not_ended`, and `rejudge.not_cancelable`.
- [ ] Add OpenAPI schemas for batch, item, create request, page and envelopes.
- [ ] Document cancellation and eventual scoreboard refresh semantics.
- [ ] Run handler tests and validate OpenAPI YAML parsing.
- [ ] Commit: `feat(api): expose rejudge batches`

### Task 10: Add Metrics And Worker Diagnostics

**Files:**
- Modify: `internal/observability/metrics.go`
- Modify: `internal/observability/metrics_test.go`
- Modify: `docs/v2-worker.md`

- [ ] Add failing metric tests for batch creation, terminal batch status and active item count.
- [ ] Add bounded labels only: target type and terminal status; never label by batch/problem/contest id.
- [ ] Document queries for stuck queued/running items and task/attempt correlation.
- [ ] Add an alert recommendation for oldest active rejudge item age without adding a noisy default threshold until trial data exists.
- [ ] Run observability tests.
- [ ] Commit: `feat(observability): expose rejudge progress`

## Chunk 6: End-To-End Verification

### Task 11: Extend Docker Smoke

**Files:**
- Modify: `deploy/smoke.sh`
- Modify: `docs/v2-deploy.md`

- [ ] Create and finish a contest with at least one accepted submission.
- [ ] Record the original attempt id and final snapshot id.
- [ ] Create a contest rejudge batch through HTTP.
- [ ] Poll with a bounded timeout until the batch is terminal.
- [ ] Assert a new attempt exists with `rejudge_batch_id` and a higher attempt number.
- [ ] Assert the batch item completed once and counters equal total count.
- [ ] Assert a newer final scoreboard snapshot exists and the public final endpoint remains valid.
- [ ] Add a cancel scenario containing at least one queued item when deterministic orchestration is available; otherwise cover cancellation at HTTP integration level.
- [ ] Update deployment documentation with recovery commands that use APIs rather than direct database mutation.
- [ ] Commit: `test(rejudge): cover batch smoke flow`

### Task 12: Full Verification And Delivery

- [ ] Run `go test ./...`.
- [ ] Run `go vet ./...`.
- [ ] Run `make compose-config`.
- [ ] Run `make compose-config-docker-runner`.
- [ ] Run `make smoke` from clean volumes.
- [ ] Run `make smoke-real-docker` when the local Docker runner environment is available.
- [ ] Run `git diff --check`.
- [ ] Review the migration for lock order, active-item uniqueness and rollback behavior.
- [ ] Review result handling for duplicate and late messages.
- [ ] Review OpenAPI examples for absent local paths or credentials.
- [ ] Push the feature branch, monitor all GitHub checks, and update the PR description with verification evidence.

## Acceptance Criteria

- Authorized operators can create a problem or ended-contest batch from HTTP.
- The batch contains a durable immutable set of terminal submissions.
- Creation is all-or-nothing across batch, items, submissions and judge tasks.
- Every selected submission receives exactly one new attempt for the batch.
- Duplicate dispatch/result messages do not duplicate attempts or progress.
- Cancel stops undispatched items and clearly reports partial completion.
- Completed ended-contest batches publish a newer final scoreboard snapshot.
- Existing non-rejudge submission, run, contest and scoreboard tests remain unchanged in behavior.
- Unit, vet, Compose, fake smoke and available real-runner smoke pass.

## Follow-Up Plans

After this plan lands, create separate implementation plans for:

1. Contest readiness and lifecycle locking.
2. Contest-level judge pause/resume and operator controls.
3. Contest announcements and clarifications.
4. Participant administration.
5. Token and float checker policies.
