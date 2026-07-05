# SOJ Insight Judge MVP Design

## Decision

SOJ Insight Judge will start inside the current SOJ repository. It should run as an independent process, but share the repository while the domain model, judge protocol, database schema, API shape, Docker deployment, and smoke workflow are still changing.

The first milestone is not a self-written Linux sandbox. The project should self-build the OJ-specific judge core: orchestration, case-level results, reproducible manifests, data validation, caching, scheduling, and observability. The low-level execution boundary should use a mature sandbox such as `isolate` or `nsjail` for the MVP.

## Product Positioning

The user-facing positioning is:

> SOJ Insight Judge: 看得懂、复现得了、比赛不炸的判题系统。

The differentiator is not "we run code ourselves". It is that SOJ makes judge behavior understandable and operable:

- Contestants see why a submission failed.
- Problem authors can validate testcase quality before publishing.
- Administrators can see judge health, queue pressure, slow languages, retries, and dead-letter tasks.
- Rejudging can explain what changed: language runtime, testcase set, checker, sandbox profile, or judge core version.

## Repository Boundary

Start in this repository:

```text
cmd/soj-judge-agent
internal/judgecore
internal/judgecore/runner
internal/judgecore/checker
internal/judgecore/manifest
deploy/
api/
```

The process boundary must still be independent:

- `soj-api` remains HTTP transport.
- `soj-worker` remains task dispatcher and submission state coordinator.
- `soj-judge-agent` performs compile/run/check work through a sandbox adapter.

Future repository extraction is allowed only after the judge protocol, release cadence, runtime images, security model, and CI needs are stable.

## MVP Capabilities

### Case-Level Result Model

The judge result must support per-case or per-group details:

- status: accepted, wrong_answer, time_limit, memory_limit, runtime_error, compile_error, output_limit, system_error
- time and memory usage
- first failed case or group
- stdout/stderr/compile output summary
- checker message
- infrastructure error classification

Submission APIs should expose safe summaries. Full internal logs and raw artifacts should remain admin-only.

### Result Ownership And Visibility

Judge Core must always produce the most complete normalized result it can. It should not decide what a contestant, contest participant, problem author, or administrator is allowed to see.

The ownership boundary is:

```text
judge-core
  -> returns full normalized result
     verdict / score / time / memory / case results / manifest / safe output summaries

submission service
  -> persists full internal result and terminal submission status

visibility policy
  -> projects API responses by actor, problem policy, contest state, and contest role
```

ACM contests still need the complete internal result model. Public ACM responses are stricter projections, not smaller Core results:

- contestants during a contest see terminal status, score semantics, timing metadata allowed by policy, and compile output summaries when appropriate
- contestants do not see hidden testcase input/output, hidden testcase names, raw checker details, or internal runner logs
- contest owners, problem authors, admins, and root users can inspect case results, manifests, and operational summaries for debugging and dispute handling
- after a contest ends, the API can optionally expose richer explanations, but this is a policy decision above Core

Core may accept execution strategy hints such as `stop_on_first_failure`, `scoring_mode`, and testcase grouping. Those hints affect execution cost and scoring semantics; they must not encode disclosure rules.

### Explainable Verdict

The first visible demo should be a failed submission page showing:

- timeline: queued -> compiling -> running -> checking -> final
- first failed group/case
- output diff summary for WA
- exit code or signal for RE
- truncated compile output for CE
- time and memory curve summary for TLE/MLE

Contest mode can hide sensitive per-case details while keeping operational metadata for admins.

### Reproducible Judge Manifest

Every judged attempt should record a manifest:

- judge core version
- sandbox adapter and profile version
- language runtime name, version, and image/rootfs digest
- compile command and run command template
- testcase set id and content hash
- checker/validator hash when present
- resource limits
- attempt id and trace/request id

This manifest is the basis for audit, rejudge, and dispute handling.

### Problem Data Check

Problem authors should be able to run a validation pass before publishing:

- testcase archive integrity
- duplicate or empty cases
- sample consistency
- missing max-size or boundary-labelled cases
- checker/validator execution errors
- suspicious output size or input size distribution

The first version can produce warnings and a score, not block publishing unless a hard integrity error exists.

### Judge Operations

Administrator-facing capabilities should include:

- queue depth and oldest waiting task
- per-language success/error/TLE/system_error rates
- worker and sandbox health
- retry and dead-letter counts
- rejudge by submission, problem, contest, language, testcase set, or judge core version
- language profile enable/disable

## Technical Architecture

`soj-worker` keeps PostgreSQL as the source of truth and Redis Stream as the delivery mechanism. `soj-judge-agent` should be called through the existing `JudgeEngine` abstraction or a compatible internal protocol.

The judge data model is defined separately in `docs/superpowers/specs/2026-07-05-soj-judge-data-model-design.md`. Implementation work must use that model as the durable schema boundary before adding case-level results, manifests, rejudge batches, problem checks, or visibility projections.

The active protocol boundary is named `soj-judge-agent.v1`. Business code should depend on `JudgeEngine`; process or repository extraction should depend on serializable agent protocol DTOs instead of database models, HTTP handlers, or worker internals. Language profiles owned by this path use the `soj-agent` engine namespace.

Initial protocol shape:

- request: protocol version, attempt id, language id, source, stdin, testcase keys, timeout, per-case limits
- result: protocol version, normalized verdict, aggregate time/memory, safe output summaries, case results, reproducibility manifest
- manifest: judge core version, sandbox profile, language runtime, testcase set hash, trace id

The agent flow:

```text
receive request
fetch source and testcase metadata
prepare local testcase cache
compile in sandbox
run cases or groups in sandbox
compare output outside the user program sandbox
emit case-level results and manifest
clean temporary files and sandbox state
return normalized judge result
```

The agent must be idempotent at the task/attempt level. Duplicate deliveries must not corrupt terminal submission state.

## Delivery Phases

The overall sequence should optimize for a running vertical slice first, then deepen correctness, safety, and product differentiation.

### Phase 1: Judge Agent Protocol Slice

Goal: prove the project no longer depends on an external judge service and can complete a submission through the internal protocol.

Deliverables:

- `cmd/soj-judge-agent` process with health endpoints
- agent protocol client behind `JudgeEngine`
- deterministic fake runner for AC, WA, CE, TLE, and system_error paths
- Docker Compose wiring for API, worker, agent, PostgreSQL, Redis, MinIO, and Prometheus
- smoke test proving submission -> worker -> agent -> terminal result without an external judge service

Parallel work units:

- protocol/client integration
- agent HTTP server and lifecycle
- fake runner and deterministic verdict fixtures
- Docker and smoke validation

### Phase 2: Durable Result Model And Visibility Policy

Goal: persist complete judge evidence while returning only safe projections to each caller.

Deliverables:

- migration update based on `docs/superpowers/specs/2026-07-05-soj-judge-data-model-design.md`
- internal result model for attempts, case results, manifests, and infrastructure errors
- database schema for full internal results and safe summary fields
- service-layer visibility policy for owner/admin/root/problem author/contest participant
- OpenAPI response fields for safe summaries
- ACM projection tests that prove hidden case details are not exposed to contestants

Parallel work units:

- schema and repository changes
- service visibility policy
- handler/OpenAPI response projection
- contest/ACM access tests

### Phase 3: Local Runner MVP

Goal: run real source code for the first language set through a replaceable runner boundary.

Deliverables:

- `internal/judgecore` domain model
- runner interface for compile, run case, check output, and cleanup
- language profiles for C++17, Go, and Python
- builtin exact-output checker with whitespace policy
- temporary workspace and artifact cleanup

Parallel work units:

- judgecore orchestration
- language profiles
- checker implementation
- workspace/artifact management

### Phase 4: Sandbox Hardening

Goal: move from local runner behavior to a defensible untrusted-code boundary.

Deliverables:

- isolate-first sandbox adapter, with nsjail as fallback if environment fit is poor
- resource limits for CPU, wall time, memory, output size, process count, fd count, and temp disk
- no-network execution profile by default
- output truncation and sanitization
- abuse regression tests for infinite loops, large output, fork attempts, memory abuse, and filesystem access

Parallel work units:

- sandbox adapter
- security profile configuration
- abuse test suite
- operational docs for judge nodes

### Phase 5: Performance And Operations

Goal: make judge throughput, tail latency, and failure modes visible and tunable.

Deliverables:

- Prometheus metrics for queue wait, compile time, run time, checker time, sandbox startup, cache hit rate, and system_error rate
- local testcase cache keyed by testcase set hash
- compile artifact cache keyed by source hash, language runtime, and compiler flags
- early stop policy for ACM-style tasks
- adaptive concurrency by language and problem cost
- admin judge operations views/API foundations for dead letters, slow languages, slow problems, and rejudge targets

Parallel work units:

- metrics and tracing hooks
- testcase cache
- compile cache
- scheduling/concurrency policy
- admin operations API

### Phase 6: Product Differentiation

Goal: turn the judge core into visible user and author value, not just an execution backend.

Deliverables:

- problem data check for testcase archive integrity, duplicate/empty cases, sample consistency, missing boundary cases, suspicious size distribution, checker errors, and validator errors
- rejudge manifest diff showing changes in language runtime, testcase set, checker, sandbox profile, and judge core version
- author-facing quality warnings before publish
- admin-facing judge reliability reports

Parallel work units:

- problem data validation engine
- manifest diff model
- author/admin API endpoints
- documentation and frontend integration contract

## Sandbox Strategy

MVP should use `isolate` first if the target environment supports it cleanly. `nsjail` is the fallback when finer configuration is needed.

Do not implement these low-level primitives in SOJ MVP:

- namespace setup
- cgroup enforcement
- seccomp policy engine
- process tree cleanup
- network isolation
- rootless user mapping

Keep the sandbox adapter interface replaceable so future backends can use gVisor, Firecracker, or another executor.

## Security Requirements

The judge agent must not run untrusted code in the API or worker process. Judge nodes should be treated as isolated, high-risk infrastructure.

MVP security rules:

- no network inside sandbox by default
- no Docker socket exposure
- no host writable mount
- no long-lived database, Redis, or S3 credentials inside sandbox
- compile, run, checker, and validator phases all get resource limits
- CPU, wall time, memory, process count, file size, output size, fd count, and temporary disk limits are enforced
- stdout, stderr, and compile output are truncated and sanitized
- expected output and hidden testcase directories are never exposed to the user program

## Performance Strategy

The performance goal is lower end-to-end judge latency and better tail behavior, not merely faster process startup.

MVP should measure:

- submit-to-final P50/P95/P99
- queue waiting P95/P99
- compile time
- sandbox startup time
- testcase preparation time
- run time by language and problem
- local testcase cache hit rate
- system_error rate

After the baseline is stable, add:

- local testcase cache keyed by testcase set hash
- compile artifact cache keyed by source hash, language runtime, and compiler flags
- early stop policy for ACM-style tasks
- adaptive concurrency by language and problem cost

## Non-Goals

For MVP, do not build:

- a fully custom sandbox
- Firecracker or gVisor production runtime
- Kubernetes-native scheduling
- support for dozens of languages
- AI judging or AI-generated problem analysis
- public multi-tenant security claims

## First Implementation Slices

1. Build the `soj-judge-agent` protocol slice with a deterministic fake runner and Docker smoke.
2. Extend result schema for attempts, case-level judge outputs, manifests, and infrastructure errors.
3. Add visibility policy so ACM contestants receive safe projections while admins/authors retain full diagnostics.
4. Add internal `judgecore` domain types and tests.
5. Support C++17, Go, and Python as initial language profiles.
6. Add exact-output checker and workspace cleanup.
7. Add `isolate` or `nsjail` runner adapter behind an interface.
8. Add API fields for explainable submission details.
9. Add Prometheus metrics for compile/run/check/cache timings.
10. Add Docker smoke for a real compile-run-check flow.

## Success Criteria

- A user can submit a wrong answer and see a useful failure explanation.
- A problem author can run testcase validation before publishing.
- An admin can identify queue pressure and judge failures from metrics.
- A judge result can be reproduced from its manifest.
- The agent protocol boundary is stable enough for future repository extraction or backend replacement.
- The security regression suite blocks common abuse cases before release.
