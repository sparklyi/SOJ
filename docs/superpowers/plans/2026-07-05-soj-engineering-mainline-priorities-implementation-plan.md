# SOJ Engineering Mainline Priorities Implementation Plan

> **For agentic workers:** Use this as a phase roadmap. Do not over-expand it before execution; split only the active phase into concrete tasks when starting that phase.

**Goal:** Move SOJ from a v2 backend with fake judge flow to a production-shaped Online Judge backend with real judging, minimum CI guardrails, and a verified Docker end-to-end path.

**Architecture:** Keep the current async judge architecture: API creates submissions, worker dispatches durable tasks, judge-agent runs JudgeCore, and result consumer persists terminal results. The next work focuses on real sandbox execution, trustworthy result evidence, and deployment safety without changing the core process boundaries.

**Tech Stack:** Go 1.24, Gin, PostgreSQL/sqlc/pgx, Redis Stream, MinIO/S3, Docker Compose, Prometheus, GitHub Actions, `isolate` sandbox.

---

## Source Spec

- `docs/superpowers/specs/2026-07-05-soj-engineering-mainline-priorities-design.md`

## Phase Order

1. P0: Minimal engineering guardrails.
2. P1: Real judge sandbox MVP.
3. P3: Real Docker end-to-end smoke.
4. P2: Trustworthy judge results.
5. P4: Production deployment closure.

P0, P1, and the smallest P3 path should be treated as the first implementation slice. P2 and P4 can follow once real execution is stable enough to validate.

## P0: Minimal Engineering Guardrails

**Purpose:** Make future judge work harder to break silently.

**Boundary:**

- Add CI and repository checks only.
- Do not change runtime behavior.
- Do not require privileged sandbox execution in CI yet.

**Complete When:**

- GitHub Actions runs on PRs and pushes to `main`.
- CI runs `go test ./...`.
- CI runs `go vet ./...`.
- CI validates `docker compose -f deploy/docker-compose.yaml config`.
- CI builds the Docker targets used by `soj-api`, `soj-worker`, `soj-judge-agent`, and `soj-migrate`.
- README or docs mention the CI expectations if needed.

**Primary Files:**

- `.github/workflows/ci.yml`
- `Dockerfile.v2`
- `deploy/docker-compose.yaml`
- `Makefile`
- `README.md`
- `README.zh-CN.md`

**Notes:**

- Keep the first CI workflow boring and reliable.
- Do not put full `deploy/smoke.sh` in required CI until sandbox/runtime requirements are stable.

## P1: Real Judge Sandbox MVP

**Purpose:** Make `soj-judge-agent` execute real Go and C++17 submissions through a production-shaped sandbox boundary.

**Boundary:**

- Implement real sandbox execution behind the existing JudgeCore/sandbox interfaces.
- Support Go and C++17 only.
- Keep fake backend for deterministic local tests.
- Keep `process` backend dev/test/local only.
- Do not add new product/API surfaces unless required to expose safe existing result fields.

**Complete When:**

- `SOJ_JUDGE_SANDBOX_BACKEND=isolate` can start a judge-agent in a production-like environment.
- Non-dev environments reject unsafe `process` backend.
- Go accepted submission returns AC through real execution.
- C++17 accepted submission returns AC through real execution.
- Compile error returns CE.
- Infinite loop returns TLE.
- Memory exhaustion returns MLE or a documented stable normalized verdict.
- Large output returns OLE.
- Abnormal exit returns RE.
- Sandbox cleanup does not leave unbounded workspaces or boxes.

**Primary Files:**

- `internal/judgecore/core.go`
- `internal/judgecore/core_test.go`
- `internal/judgecore/language/language.go`
- `internal/judgecore/sandbox/backend.go`
- `internal/judgecore/sandbox/backend_test.go`
- `internal/judgecore/sandbox/sandbox.go`
- `internal/judgecore/checker/checker.go`
- `internal/judgecore/checker/checker_test.go`
- `internal/app/judge_agent.go`
- `cmd/soj-judge-agent/main.go`
- `deploy/docker-compose.yaml`
- `Dockerfile.v2`

**Notes:**

- Treat compile as untrusted execution.
- Keep verdict normalization centralized.
- Prefer table-driven tests for sandbox status mapping.
- Do not let judge-agent receive business database credentials.

## P3: Real Docker End-To-End Smoke

**Purpose:** Prove the real judging path works through the complete local stack.

**Boundary:**

- Extend Docker Compose and smoke testing for real Go/C++17 submissions.
- Preserve the existing fake smoke path for fast deterministic validation.
- Do not require production-grade deployment hardening in this phase.

**Complete When:**

- Clean Compose startup can run API, worker, judge-agent, PostgreSQL, Redis, MinIO, and Prometheus.
- A real Go or C++17 submission flows through:
  `submit -> dispatcher -> judge-agent -> result consumer -> PostgreSQL -> API projection`.
- Smoke test validates at least one AC result.
- Smoke test validates at least one non-AC verdict if the runtime is available.
- Smoke test verifies result stream and terminal database state.
- Metrics remain available for API, worker, and judge-agent.

**Primary Files:**

- `deploy/docker-compose.yaml`
- `deploy/smoke.sh`
- `deploy/prometheus.yml`
- `Dockerfile.v2`
- `README.md`
- `README.zh-CN.md`
- `docs/v2-deploy.md`

**Notes:**

- If `isolate` requires privileged Docker settings, document them clearly.
- Keep fake smoke available because it is cheaper and less environment-sensitive.

## P2: Trustworthy Judge Results

**Purpose:** Make judge results explainable, reproducible, and safe to expose.

**Boundary:**

- Improve stored evidence and API projection around existing judge result model.
- Do not implement custom Special Judge sandboxing yet.
- Do not expose hidden testcase data to contestants.

**Complete When:**

- Case-level results are persisted and idempotent.
- Compile output is summarized safely.
- First failed case or group is available where policy allows.
- Checker messages are safely summarized.
- Runtime fingerprint is captured.
- Judge manifest records judge core version, sandbox backend/profile, language runtime, testcase set hash, checker metadata, limits, attempt id, and trace/request id.
- Contest/frozen visibility tests prove hidden details stay hidden.

**Primary Files:**

- `internal/submission/repository.go`
- `internal/submission/service.go`
- `internal/submission/handler.go`
- `internal/submission/service_test.go`
- `internal/postgres/queries/submissions.sql`
- `internal/postgres/db/*.go`
- `internal/judge/protocol.go`
- `internal/judge/events/events.go`
- `api/openapi.yaml`
- `docs/v2-api-guide.md`

**Notes:**

- JudgeCore should produce full normalized evidence.
- Submission/visibility policy decides what each actor can see.
- Late results must not overwrite newer submission projections.

## P4: Production Deployment Closure

**Purpose:** Make the real judge path understandable and safer to operate.

**Boundary:**

- Document and enforce deployment constraints.
- Add only lightweight runtime checks needed to prevent dangerous configuration.
- Do not build a full operations console yet.

**Complete When:**

- Deployment docs distinguish local fake, dev/test process, and production-like isolate backends.
- Secret handling is documented for JWT, PostgreSQL, Redis, and object storage.
- Judge-agent credential boundary is explicit.
- `/metrics` exposure guidance is documented.
- Sandbox backend safety matrix exists.
- Troubleshooting covers queue backlog, missing result events, agent startup failures, and sandbox verdict anomalies.

**Primary Files:**

- `docs/v2-deploy.md`
- `docs/v2-worker.md`
- `README.md`
- `README.zh-CN.md`
- `deploy/env/api.env.example`
- `deploy/config.example.yaml`
- `internal/config/config.go`
- `internal/app/judge_agent.go`

**Notes:**

- Keep docs aligned with actual config names.
- Do not document production defaults that are not enforced by code.

## First Execution Slice

Start with this slice:

- [ ] P0 CI workflow.
- [ ] P1 isolate backend skeleton and safety checks.
- [ ] P1 Go/C++17 profile validation through real execution.
- [ ] P3 minimal real Docker smoke for one accepted submission.

Do not start P2 result expansion until at least one real language can be judged end to end. Do not start P4 docs closure until the real Docker flow is known.

## Phase Commit Strategy

- P0: `ci: add backend validation workflow`
- P1 sandbox skeleton: `feat(judgecore): add isolate sandbox backend`
- P1 language execution: `feat(judgecore): run go and cpp submissions in sandbox`
- P3 smoke: `test(deploy): add real judge smoke path`
- P2 evidence: `feat(judge): persist trustworthy judge evidence`
- P4 docs/config: `docs(deploy): document production judge path`

Each phase should end with:

- `go test ./...`
- `go vet ./...`
- relevant Docker Compose or smoke validation for touched deployment paths
- a clean git status
