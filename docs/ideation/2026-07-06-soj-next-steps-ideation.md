---
date: 2026-07-06
topic: soj-next-steps
focus: open-ended project next step after syncing main
---

# Ideation: SOJ Next Steps

## Codebase Context

SOJ v2 is now a Go 1.24 online judge backend with REST APIs, PostgreSQL/sqlc, Redis Stream judge delivery, S3-compatible object storage, Docker/gVisor runner support, Prometheus metrics, worker and judge-agent readiness probes, dead-task recovery, and contest scoreboard snapshots.

The latest `main` is already up to date at `105d84b` after PR #22. Recent merges focused on Docker/gVisor runner, runtime readiness, and scoreboard snapshots. There are no current `TODO` or `FIXME` markers in the repository. Local quick checks passed on 2026-07-06:

- `go test ./...`
- `go vet ./...`
- `docker compose -f deploy/docker-compose.yaml config`
- `COMPOSE_FILE=deploy/docker-compose.yaml:deploy/docker-compose.docker-runner.yaml docker compose config`

The strongest current signals:

- README roadmap and deployment docs explicitly name OpenTelemetry tracing as the intended next observability phase.
- CI validates unit tests, vet, Compose config, and image builds, but does not run the existing end-to-end smoke path.
- Metrics exist, but there are no checked-in dashboards or alert rules.
- Database/sqlc already include `problem_check_runs`, `problem_check_findings`, and `rejudge_batches`, but service/routes do not expose those workflows yet.
- OpenAPI exists and is documented, but there is no contract-conformance or frontend client generation gate.

## Ranked Ideas

### 1. Add End-To-End OpenTelemetry Tracing

**Description:** Add optional OpenTelemetry tracing with OTLP export disabled by default. Carry trace context from HTTP request creation through submission creation, judge task dispatch, Redis request/result events, judge-agent execution, result consumption, and contest projection.

**Rationale:** This directly matches the documented roadmap and closes the main blind spot in the async judge pipeline. The code already persists `trace_id` in judge attempts and event payloads, so the repo has a natural landing zone.

**Downsides:** Requires careful dependency and config design so local development remains simple. Trace propagation across Redis events needs stable conventions.

**Confidence:** 92%

**Complexity:** Medium

**Status:** Explored

### 2. Promote Smoke Tests Into CI And Release Gates

**Description:** Add CI jobs or manually triggered workflows for the existing fake async smoke path, then a separate runner smoke using published GHCR images when environment support is available. Keep the current unit/vet/image checks as the fast path.

**Rationale:** The repository already has `deploy/smoke.sh`, `make smoke-real-docker`, and Docker Compose wiring. CI currently stops before proving API + worker + judge-agent + Redis + PostgreSQL + MinIO cooperate as a running system.

**Downsides:** Docker integration jobs can be slower and flakier than unit tests. Real runner smoke may need careful opt-in gating.

**Confidence:** 88%

**Complexity:** Medium

**Status:** Deferred

### 3. Add Production Trial Dashboards And Alert Rules

**Description:** Check in Prometheus alert rules and a minimal dashboard spec covering readiness failures, queue backlog, oldest pending age, judge task error/dead counts, result-consumer failures, judge-agent slot saturation, sandbox errors, cleanup failures, and HTTP latency.

**Rationale:** Runtime metrics are already present and documented, but operators still need concrete signals and thresholds before a serious trial deployment.

**Downsides:** Thresholds will initially be approximate until real traffic data exists. Dashboard format choice can add maintenance overhead.

**Confidence:** 84%

**Complexity:** Low to Medium

**Status:** Explored

### 4. Expose Problem Data Validation Workflow

**Description:** Implement a problem check service/API around the existing `problem_check_runs` and `problem_check_findings` schema. Start with testcase archive validation, input/output pairing, sample consistency, case count verification, and publish-blocking findings.

**Rationale:** Online judge quality depends heavily on problem data correctness. The database/query layer already anticipates this feature, but authors currently only get basic upload and publish checks.

**Downsides:** The scope can grow quickly if it absorbs validators, special judge, and language-specific probes too early.

**Confidence:** 82%

**Complexity:** Medium

**Status:** Unexplored

### 5. Implement Rejudge Batch Operations

**Description:** Add admin/owner rejudge APIs and worker support for creating, listing, canceling, and progressing rejudge batches using the existing `rejudge_batches` schema and `judge_attempts.rejudge_batch_id`.

**Rationale:** Once real judging is running, rejudge is a core operational capability for changed test data, fixed checkers, changed language profiles, and incident recovery. The schema is already in place.

**Downsides:** Needs careful idempotency, contest scoreboard interactions, and visibility semantics. It should follow tracing/smoke work rather than precede it.

**Confidence:** 79%

**Complexity:** High

**Status:** Unexplored

### 6. Add OpenAPI Contract Conformance And Client Generation

**Description:** Add a CI check that validates `api/openapi.yaml` against registered routes and response envelope conventions, then optionally generate a typed frontend client artifact.

**Rationale:** The project advertises OpenAPI as the frontend integration contract. Contract drift will become more expensive as frontend or third-party users appear.

**Downsides:** Route/schema conformance checks in Go can require either a new tool dependency or pragmatic generated smoke cases.

**Confidence:** 74%

**Complexity:** Medium

**Status:** Unexplored

### 7. Add Production Config Fail-Fast Guards

**Description:** Fail startup in `SOJ_ENV=prod` when critical secrets or deployment choices are unsafe: empty or local JWT secret, local object-storage credentials, fake judge endpoint, unpinned runner image tags, or metrics exposure assumptions that must be enforced at ingress.

**Rationale:** Docs warn about these risks, but some are not enforced centrally at API/worker startup. Fail-fast checks reduce accidental unsafe deployments.

**Downsides:** Overly strict checks can block legitimate staging environments, so the policy must distinguish `prod` from `local`, `dev`, and `test`.

**Confidence:** 72%

**Complexity:** Low to Medium

**Status:** Unexplored

## Rejection Summary

| # | Idea | Reason Rejected |
|---|------|-----------------|
| 1 | Build a frontend first | Not grounded enough in this repository, which is currently backend and deployment heavy. |
| 2 | Add more judge languages immediately | Useful later, but current Go/C++17 path should first get stronger observability and trial gates. |
| 3 | Replace Docker/gVisor with another sandbox | Too expensive and duplicates the just-completed production sandbox direction. |
| 4 | Build a custom scheduler service | Premature relative to current slot limiter, Redis stream pipeline, and unknown real load. |
| 5 | Add Kubernetes manifests now | Docker Compose is the documented deployment target; trial operability should come first. |
| 6 | Implement special judge execution | Valuable but higher-risk; problem data validation should establish the workflow first. |
| 7 | Add distributed cache for testcase archives | Not enough evidence of performance pain yet. |
| 8 | Rewrite repositories away from sqlc | Not justified; current sqlc boundary is consistent and tested. |
| 9 | Add GraphQL | Not grounded in current API style or OpenAPI-first contract. |
| 10 | Expand scoreboard features | Recently improved with snapshots; weaker than tracing and operations next. |
| 11 | Add more CRUD endpoints | Too generic; existing CRUD surface is already broad for v2 backend. |
| 12 | Build a plugin system | Too vague and not connected to the current codebase risks. |
| 13 | Add load testing as the top item | Useful, but existing capacity smoke plus missing tracing/alerts suggests observability should come first. |
| 14 | Refactor module layout | No evidence the current layout is blocking progress. |
| 15 | Add external issue mining | The prompt did not request issue-tracker analysis, and repo signals were sufficient for this pass. |

## MVP Recovery Pass: 2026-07-09

### Updated Grounding

Latest `main` is at `64eafad` after PR #24. Since the original pass, the top observability and release-gate items have mostly landed:

- Optional OpenTelemetry tracing and the trial diagnostic loop are implemented and documented.
- Prometheus alert rules are checked in under `deploy/prometheus-rules/soj-alerts.yml`.
- CI now includes a Docker smoke gate.
- Scoreboard snapshots and judge runtime readiness operations are on main.

The stronger MVP question is no longer "what infrastructure should be next?" It is "what is the smallest set of product and operator workflows needed to make the v2 backend usable by the separate `soj-web` frontend after the v1 archive split?"

Comparing current v2 to `origin/archive/v1` shows these removed or unexposed user-facing areas:

- Email verification/login and captcha.
- User profile update, avatar upload, password update/reset, and user detail pages.
- User-owned problem listing.
- Per-problem submission ranking.
- Contest registration/application management beyond self-registration.
- Testcase-level management endpoints; v2 currently prefers versioned testcase archive uploads.

Current v2 also has tables and sqlc queries for `problem_check_runs`, `problem_check_findings`, and `rejudge_batches`, but the service and route layers do not expose them yet.

### Recommended MVP Order

#### P0. Align the backend contract with `soj-web`

The first-party frontend lives in the separate remote `soj-web` project, not this repository. For MVP, inspect `soj-web` and align the backend contract around the screens it needs: login/register, problem list/detail, submit, submission detail, run/self-test, contest list/detail, registration, scoreboard, and an admin/author surface for creating problems, uploading statements/testcase archives, publishing, syncing languages, and checking smoke health.

This should come before restoring most legacy extras because the frontend will reveal the real API gaps and make the backend demonstrable.

After cloning `git@github.com:sparklyi/soj-web.git`, the frontend's own `docs/development/openapi-inventory.md` and `docs/development/api-integration-smoke.md` show the current aligned surface more precisely:

- HTTP mode already adapts auth, problems, submissions, self-runs, contests, contest registration, and ACM scoreboard.
- The most urgent backend contract gap is a public enabled-language catalog. `soj-web` currently calls `/api/v1/admin/languages`, which blocks regular users from completing submissions in HTTP mode.
- Contest list/detail should expose current-user registration state, contest scoring mode, and enriched contest problem titles.
- OI/IOI scoreboard fields and Arena event feed fields remain mock-only unless those product scopes stay in the MVP.

#### P0. Expose problem data validation

Implement a problem check service/API over the existing `problem_check_runs` and `problem_check_findings` schema. Minimum scope: validate testcase archive structure, input/output pairing, case count, sample consistency where available, storage readability, and publish-blocking error findings.

This is the highest backend-only feature because bad test data makes an OJ unusable even if judging technically works.

#### P1. Implement rejudge batch operations

Expose admin/owner rejudge APIs over the existing `rejudge_batches` schema and wire worker support to create new attempts for affected submissions. Minimum scope: create/list/detail/cancel batches by problem or contest, progress counters, and idempotent task creation.

This restores a core OJ operation for changed testcases, checker fixes, language/runtime changes, and incident recovery.

#### P1. Restore user self-service profile basics

Add current-user update endpoints for username, bio, avatar URL/upload path, and password change. Avoid email-login and reset-password until an email verification channel is intentionally brought back.

This recovers low-cost v1 functionality because v2 already stores `avatar_url` and `bio`.

#### P1. Add problem owner and per-problem ranking views

Expose "my problems" and a per-problem accepted ranking/time-memory leaderboard. These were present in v1 and matter for author workflows and user motivation, but they can follow the frontend and validation path.

#### P2. Add OpenAPI/client contract gates

Once the frontend exists, add route/schema conformance checks and generate a typed client from `api/openapi.yaml`. This prevents drift between the MVP app and backend.

#### P2. Defer legacy email/captcha/application extras

Email login, captcha, password reset, notification preferences, and a richer contest application review workflow should stay out of the first MVP recovery slice unless a public deployment requires abuse prevention immediately.

## Session Log

- 2026-07-06: Initial ideation -- 22 candidates considered, 7 survived. Latest `main` was already up to date and quick local verification passed.
- 2026-07-06: User selected ideas 1 and 3 for the next scope; CI smoke/release-gate work from idea 2 was explicitly deferred.
- 2026-07-09: Synced to `origin/main` at `64eafad`, compared active v2 against `origin/archive/v1`, and reframed the next work around MVP recovery rather than more infrastructure.
- 2026-07-09: User clarified that the frontend is the separate remote `soj-web` project; updated P0 from building a new frontend to aligning backend APIs with that frontend.
