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

1. Extend result schema for case-level judge outputs and manifests.
2. Add internal `judgecore` domain types and tests.
3. Add `soj-judge-agent` with fake local runner for deterministic smoke tests.
4. Add `isolate` or `nsjail` runner adapter behind an interface.
5. Support C++17, Go, and Python as initial language profiles.
6. Wire `JudgeEngine` to call the agent while keeping Judge0 fallback.
7. Add API fields for explainable submission details.
8. Add Prometheus metrics for compile/run/check/cache timings.
9. Add Docker smoke for a real compile-run-check flow.

## Success Criteria

- A user can submit a wrong answer and see a useful failure explanation.
- A problem author can run testcase validation before publishing.
- An admin can identify queue pressure and judge failures from metrics.
- A judge result can be reproduced from its manifest.
- The agent can be disabled and traffic can fall back to Judge0.
- The security regression suite blocks common abuse cases before release.
