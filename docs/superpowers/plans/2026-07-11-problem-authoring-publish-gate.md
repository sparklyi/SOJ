# Problem Authoring And Publish Gate Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Let an authenticated problem owner create, edit, validate, and publish a judge-ready problem from SOJ-web while SOJ rejects publication unless the current statement and testcase set have a completed valid check.

**Architecture:** SOJ exposes an owner/admin-only authoring-state endpoint that aggregates the current statement, testcase set, latest relevant check, and publish blockers. Check runs bind both content versions, and the publish path reuses the same readiness evaluation while holding the problem update lock. SOJ-web extends its API adapter and adds browser-authenticated authoring pages that follow the existing Signal Arena design system.

**Tech Stack:** Go 1.25, Gin, PostgreSQL/sqlc, OpenAPI, Next.js 16, React 19, TypeScript, Vitest, Playwright.

---

## Chunk 0: Branch And Baseline

- [x] Fetch both repositories and confirm clean `main` branches match `origin/main`.
- [x] Create backend branch `feat/problem-authoring-publish-gate` from `origin/main` in an isolated worktree.
- [x] Create frontend branch `feat/problem-authoring-workspace` from `origin/main` in an isolated worktree.
- [x] Configure both repositories with commit email `sparkyi@foxmail.com`.
- [x] Run baseline `go test ./...` and `npm run ci:fast` successfully before implementation.

## Chunk 1: Backend Publish Integrity

### Task 1: Define Authoring Readiness Behavior

**Files:**
- Modify: `internal/problem/service_test.go`
- Modify: `internal/problem/service.go`
- Modify: `internal/problem/repository.go`
- Modify: `internal/postgres/queries/problems.sql`
- Regenerate: `internal/postgres/db/problems.sql.go`
- Regenerate: `internal/postgres/db/querier.go`
- Modify: `internal/postgres/db/submissions_sql_test.go`

- [x] Add failing tests proving publication fails without a check, with a stale testcase-set check, and with an invalid check.
- [x] Add a passing test proving a completed valid check for the current testcase set permits publication.
- [x] Add repository coverage for selecting the latest completed check by `problem_id + statement_id + testcase_set_id`.
- [x] Cover a testcase-set switch after check completion and serialize publish/upload through the problem row lock.
- [x] Implement a single readiness evaluator used by both authoring state and `ensurePublishable`.
- [x] Run `go test ./internal/problem`.

### Task 2: Expose Authoring State

**Files:**
- Modify: `internal/problem/service_test.go`
- Modify: `internal/problem/service.go`
- Modify: `internal/problem/handler_test.go`
- Modify: `internal/problem/handler.go`
- Modify: `internal/problem/routes.go`
- Modify: `api/openapi.yaml`
- Modify: `docs/v2-api-guide.md`

- [x] Add failing service and handler tests for owner/admin access and nullable incomplete state.
- [x] Treat only explicit not-found statement/testcase/check results as incomplete; propagate infrastructure errors.
- [x] Add `GET /api/v1/problems/{id}/authoring`.
- [x] Return problem, optional current statement, optional current testcase set, optional latest current-set check, `publishable`, and stable blocker codes.
- [x] Document the endpoint and schemas in OpenAPI/API guide.
- [x] Run backend focused and full checks.

### Task 3: Add Owner-Only Problem Listing

**Files:**
- Modify: `internal/problem/service_test.go`
- Modify: `internal/problem/service.go`
- Modify: `internal/problem/handler_test.go`
- Modify: `internal/problem/handler.go`
- Modify: `internal/postgres/queries/problems.sql`
- Modify: `internal/problem/repository.go`
- Regenerate: `internal/postgres/db/problems.sql.go`
- Modify: `api/openapi.yaml`
- Modify: `docs/v2-api-guide.md`

- [x] Add failing authorization tests proving `mine=true` requires authentication and scopes results to `actor.UserID`, including for admins.
- [x] Add an explicit owner filter to repository list/count queries instead of relying on visibility filtering.
- [x] Document `mine=true` and consume it from the authoring workspace.
- [x] Run focused problem and PostgreSQL query tests.

## Chunk 2: Frontend Authoring Workspace

### Task 4: Extend The API Boundary

**Files:**
- Modify: `lib/api/types.ts`
- Modify: `lib/api/backend-types.ts`
- Modify: `lib/api/http-adapter.ts`
- Modify: `lib/api/mock-adapter.ts`
- Modify: `tests/unit/http-adapter.test.ts`
- Modify: `tests/unit/mock-adapter.test.ts`

- [x] Add failing adapter tests for create, update, statement save, testcase upload, check execution, and authoring-state retrieval.
- [x] Implement typed DTO mappings and multipart upload.
- [x] Keep mock mode deterministic for UI development.
- [x] Run focused unit tests.

### Task 5: Build Authoring Pages

**Files:**
- Create: `app/manage/problems/page.tsx`
- Create: `app/manage/problems/[id]/page.tsx`
- Create: `features/problems/authoring/problem-manager.tsx`
- Create: `features/problems/authoring/problem-workbench.tsx`
- Create: `features/problems/authoring/problem-authoring-form.tsx`
- Create: `features/problems/authoring/problem-check-panel.tsx`
- Modify: `components/layout/top-nav.tsx`
- Modify: `tests/unit/feature-api.test.ts`
- Create: `tests/e2e/problem-authoring.spec.ts`

- [x] Write failing component/unit tests for authoring states and commands.
- [x] Implement the authenticated list/create page.
- [x] Consume the authenticated `mine=true` list so authoring never exposes edit actions for another owner's public problems.
- [x] Implement metadata and statement editing, testcase upload, check findings, and publish controls.
- [x] Add role-aware navigation without exposing author commands to guests.
- [x] Add browser E2E coverage.

## Chunk 3: Real Integration And Delivery

### Task 6: Cross-Repository HTTP Smoke

**Files:**
- Create: `SOJ-web/playwright.http.config.ts`.
- Create: `SOJ-web/scripts/http-authoring-e2e.mjs` if stack orchestration is needed.
- Create: `SOJ-web/tests/http/problem-authoring-http.spec.ts`.
- Modify: `SOJ-web/package.json`.
- Modify: `SOJ-web/docs/development/api-integration-smoke.md`.

- [x] Add a repeatable command that starts/waits for SOJ, starts SOJ-web with `NEXT_PUBLIC_SOJ_API_MODE=http`, and cleans up owned processes/data.
- [x] Generate valid and invalid testcase zip fixtures during the test without committing binary archives.
- [x] Use Playwright to create, populate, validate, publish, and submit a problem through the browser.
- [x] Verify invalid data cannot publish and valid data can.

### Task 7: Verification And Publication

- [x] Run `go test ./...`, `go vet ./...`, Compose validation, and backend smoke.
- [x] Run `npm run ci` in SOJ-web.
- [x] Review Go functional changes and frontend screenshots at desktop/mobile sizes.
- [x] Reconfirm both feature branches still have the expected merge base before push.
- [x] Commit with `sparkyi@foxmail.com`, push both branches, create linked GitHub PRs, cross-link them, note backend-first merge/deploy order, and monitor all checks to completion.
