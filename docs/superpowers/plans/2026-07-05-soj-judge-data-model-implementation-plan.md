# SOJ Judge Data Model Implementation Plan

> **For agentic workers:** Use `subagent-driven-development` or `executing-plans` when implementing this plan. Keep commits phase-based and update docs at each milestone.

## Goal

Build the durable judge data foundation before adding the local runner, sandbox hardening, cache, or rejudge execution. The schema must support long-term ACM/非 ACM visibility, rich internal diagnostics, rejudge history, problem quality checks, and later extraction into an independent judge service without reshaping tables again.

## Design Boundary

- `submissions` / `runs`: user-facing request facts.
- `judge_attempts`: one durable judge execution fact per attempt.
- `judge_case_results`: normalized per-case evidence owned by an attempt.
- `submission_results`: current public/service projection only, never the evidence source of truth.
- `contest_problem_results`: scoreboard projection only, with references to winning/latest attempt.
- `artifacts`: shared binary/text artifact registry extended to support attempt/case outputs and manifests.
- Visibility is a service/API policy, not a persistence shortcut. Core persists complete normalized results; API projections decide what contestants, owners, admins, and ACM participants can see.

## Parallel Phase Split

### Phase 2A: Schema Contract

Status: completed.

Owner scope:
- Reshape `internal/migrations/000001_init.up.sql`.
- Add `judge_attempts`, `judge_case_results`, reshaped `submission_results`, contest attempt references, artifact owner/kind extensions, and required indexes.
- Because the project is not launched, edit the initial migration directly instead of adding compatibility migrations.

Expected commit:
- `feat(judge): add durable judge result schema`

### Phase 2B: sqlc Query Layer

Status: completed.

Owner scope:
- Add attempt, case-result, current-projection, and contest-projection queries under `internal/postgres/queries/`.
- Regenerate `internal/postgres/db/*.go`.
- Keep query APIs typed and aligned with schema names.

Depends on:
- Phase 2A schema names.

Expected commit:
- `feat(judge): add judge result sqlc queries`

### Phase 2C: Repository Transaction

Status: completed.

Owner scope:
- Add typed repository records for attempts, cases, and current result projection.
- Implement one transactional completion path that updates submission terminal state, creates attempt/cases, upserts current projection, and updates contest projection.
- Keep memory repository parity for service tests.

Depends on:
- Phase 2B generated query layer.

Expected commit:
- `feat(judge): persist attempts and case results`

### Phase 2D: Visibility And API Projection

Status: completed.

Owner scope:
- Add service-level submission result visibility projection.
- Add tests for owner/admin/contestant/ACM visibility.
- Update handler/service DTOs, `api/openapi.yaml`, and `docs/v2-api-guide.md`.
- Do not expose raw manifests or hidden testcase identities by default.

Can start after:
- Phase 2C repository read models are clear.

Expected commits:
- `feat(judge): add result visibility policy`
- `feat(api): expose safe judge result summaries`

### Phase 2E: Future-Proof Control Tables

Status: completed.

Owner scope:
- Add `problem_check_runs`, `problem_check_findings`, and `rejudge_batches`.
- Add nullable `rejudge_batch_id` linkage on `judge_attempts`.
- Add minimal create/read/progress queries only; do not implement validator execution or rejudge fanout in this phase.

Can run in parallel with:
- Phase 2C/2D after Phase 2A settles table naming.

Expected commit:
- `feat(judge): add problem check and rejudge tables`

### Phase 2F: Integration Verification And Docs

Owner scope:
- Run full Go checks and Docker Compose validation.
- Verify empty DB migration.
- Scan for anti-deformation patterns such as generic judge `details jsonb` extensions on projection tables.
- Update architecture docs and README if the implemented boundary differs from the design docs.

Runs after:
- All implementation phases.

Expected commit:
- `docs(judge): document durable result schema`

## Verification Gates

Each implementation phase must pass the narrowest relevant test before commit. The final phase must pass:

```bash
go test ./...
go vet ./...
docker compose -f deploy/docker-compose.yaml config
```

Final verification also needs an empty PostgreSQL migration run through the project migration path.

## Done Criteria

- Durable judge facts live in `judge_attempts` and `judge_case_results`.
- `submission_results` is only the current-result projection and references an attempt.
- Submission completion persists terminal state, attempt, case results, current projection, and contest projection in one transaction.
- ACM visibility is enforced by projection policy, not by deleting or skipping internal evidence.
- OpenAPI and API guide document public/admin result shapes.
- Problem-check and rejudge roots exist without forcing later table deformation.
- Full local checks and Docker validation pass.

## Deferred Work

- Real `soj-judge-agent` execution.
- Sandbox hardening with Docker runner + gVisor/runsc; `isolate`/`nsjail` remain future backend options.
- Judge cache and performance baselines.
- Problem validator runtime.
- Rejudge queue fanout and worker orchestration.
