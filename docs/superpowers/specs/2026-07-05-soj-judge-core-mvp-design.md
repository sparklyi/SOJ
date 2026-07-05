# SOJ Judge Core MVP Design

## Decision

SOJ Judge Core MVP 采用 **模块化 Judge Core + 异步 Judge Agent + 预留 Scheduler** 的设计。

生产链路不使用同步 RPC 等待判题结果。API 创建提交后，任务进入事件流；`soj-judge-agent` 作为独立进程消费判题请求，执行 compile/run/check，再发布结构化结果事件；SOJ 主系统通过 result consumer 事务落库并更新当前结果投影。

Judge Core 的第一阶段目标不是自研 Linux sandbox，而是自研 OJ 领域核心能力：

- 语言 profile 和 toolchain 指纹。
- 编译、运行、checker、聚合、manifest 的稳定 pipeline。
- case-level result、可复现证据和安全摘要。
- testcase 本地只读缓存和 hash 校验。
- 可替换 sandbox backend，生产默认 `docker` + `runsc`/gVisor。
- 面向比赛峰值的异步削峰、横向 agent 扩展和可观测性。

## Goals

MVP 按校赛/区域赛规模设计：

- 支撑 `1k-5k` 并发用户。
- 支撑 `5k-20k` 题量。
- 支撑峰值数百次提交/分钟。
- 支持 agent 横向扩展。
- 支持 Go 和 C++17 首批真实代码执行。
- 支持 AC/WA/CE/RE/TLE/MLE/OLE/system_error 的稳定归一。
- 支持完整内部证据持久化和上游可见性裁剪。

长期边界需要预留到平台级规模，但 MVP 不直接实现独立调度集群、远程执行池、冷热数据分区或完整 Special Judge 沙箱执行。

## Non-Goals

MVP 不做以下事项：

- 不让 API 或 worker 同步等待长耗时判题。
- 不让 judge agent 直接写业务数据库。
- 不把源码、大输出、大日志或 testcase 原文放进 Redis Stream。
- 不把 ACM/OI/自测展示规则写进 Judge Core。
- 不把 production 默认执行 backend 设为裸 `process`。
- 不在第一版实现完整独立 Scheduler 服务。
- 不在第一版实现用户自定义 Special Judge 的沙箱执行。

## Prerequisites

Judge Core MVP builds on the durable judge data model that already defines `judge_attempts`, `judge_case_results`, `submission_results` as current projection, and contest result attempt references.

Implementation must treat this data model as a prerequisite, not an optional later enhancement:

- dispatcher creates or references a durable `judge_attempts.id` before publishing `judge.request.v1`
- result consumer persists attempt summary, case results, current projection and contest projection in one business transaction
- late result guards compare `attempt_id` against the current submission projection before updating public summary
- duplicate terminal results use attempt-level idempotency and do not duplicate case rows

If a local branch does not yet contain the durable data model, Phase 0 must first port the data-model migration and repository transaction before implementing async execution.

## Architecture

核心组件边界：

```text
soj-api
  -> create submission / query safe projection

soj-worker / dispatcher
  -> create or claim judge task
  -> publish judge.request.v1

soj-judge-agent
  -> consume judge.request.v1
  -> run judgecore pipeline
  -> publish judge.result.v1

result-consumer
  -> consume judge.result.v1
  -> persist attempt, case results, summary, contest projection transactionally

judgecore
  -> language profiles
  -> testcase cache
  -> sandbox adapter
  -> runner pipeline
  -> checker
  -> artifact and manifest builders
```

Production event flow:

```text
submit code
  -> API creates submission + judge_task + judge_attempt
  -> dispatcher publishes judge.request.v1
  -> judge-agent consumer group claims request
  -> judgecore resolves source, testcase set, language profile
  -> sandbox compiles source
  -> sandbox runs testcase groups
  -> checker compares output
  -> agent publishes judge.result.v1
  -> result-consumer persists result in one business transaction
  -> API returns safe projection based on visibility policy
```

The Scheduler is a future service boundary, not an MVP process. Events and metadata must still carry enough information to support later language queues, priority queues, resource-aware placement, and agent pools.

## Migration From Current Worker

The current worker path synchronously calls `JudgeEngine.Judge` and then calls `CompleteSubmission`. That path is incompatible with the production async design and must be treated as a transitional compatibility path.

Phase 0 should split the current behavior into three roles:

- **dispatcher**: claims pending judge tasks, creates or loads the active attempt, writes a durable outbound event record, and publishes `judge.request.v1`
- **agent**: consumes request events and publishes result events; it does not call `CompleteSubmission`
- **result consumer**: consumes `judge.result.v1` and calls the repository transaction that completes attempts, case results, current projection and contest projection

`JudgeEngine` can remain for deterministic fake tests and local direct-run adapters, but production submission judging must not use `ProcessMessage -> Judge -> CompleteSubmission` as the final path.

The compatibility adapter may be retained only behind an explicit dev/test config flag while the async path is being built.

## Package Layout

Suggested first layout:

```text
cmd/soj-judge-agent
internal/judgecore
internal/judgecore/language
internal/judgecore/sandbox
internal/judgecore/runner
internal/judgecore/checker
internal/judgecore/testcase
internal/judgecore/artifact
internal/judgecore/manifest
internal/judgecore/events
```

`internal/judgecore` owns execution semantics. It must not import API handlers, contest services, submission services, database repositories, Gin context, or Redis-specific infrastructure.

## Design Patterns

Judge Core should use these patterns deliberately:

- **Ports and Adapters**: core logic depends on interfaces for sandbox, testcase store, artifact store, events, and language registry.
- **Pipeline Orchestrator**: one attempt flows through `prepare -> compile -> run cases -> check -> aggregate -> manifest -> cleanup`.
- **Strategy Registry**: language profiles, checker policies, and sandbox backends are registered strategies, not `switch` blocks spread across runner code.
- **Factory**: runtime config builds the correct sandbox, artifact store, testcase cache, and checker implementation.
- **Result Normalizer**: all adapter-specific errors become stable judge verdicts and platform error classes.
- **Context First**: every IO-bound or long-running operation accepts `context.Context`; Gin context must not leak into core code.

## Core Interfaces

Conceptual interfaces:

```text
JudgeCore
  Judge(ctx, JudgeRequest) (JudgeResult, error)

LanguageRegistry
  Resolve(ctx, language_id or slug) (LanguageProfile, error)

LanguageProfile
  CompilePlan() CompilePlan
  RunPlan() RunPlan
  DefaultLimits() ResourceLimits
  RuntimeFingerprint(ctx) RuntimeFingerprint

Sandbox
  Prepare(ctx, SandboxRequest) (Workspace, error)
  Compile(ctx, Workspace, CompilePlan) (CompileResult, error)
  Run(ctx, Workspace, RunPlan, CaseInput) (RunResult, error)
  Cleanup(ctx, Workspace) error

Checker
  Check(ctx, CheckRequest) (CheckResult, error)

TestcaseStore
  ResolveAndCache(ctx, TestcaseSetRef) (CachedTestcaseSet, error)

ArtifactStore
  Put(ctx, Artifact) (ArtifactRef, error)
  Get(ctx, ArtifactRef) (Artifact, error)

ManifestBuilder
  Build(ctx, ManifestInput) Manifest
```

The runner should consume `LanguageProfile` and `ResourceLimits`; it should not know language-specific compile commands by hardcoded conditionals.

## Language Model

MVP supports Go and C++17.

Python 3 is intentionally moved out of the Core MVP. Existing phase docs that mention Python should be interpreted as post-MVP language expansion unless a later implementation plan reintroduces it explicitly.

Language definitions must be data-driven or profile-driven:

- `id`, `slug`, `display_name`
- compile command template
- run command template
- output binary or artifact path
- default CPU, wall, memory, output, process and fd limits
- runtime version probe command
- image/rootfs/profile version
- environment variables allowlist

Future languages should be added by registering a new profile and tests. Python, Java, Rust, TypeScript, and custom runtimes should not require changing the judge pipeline.

## Checker Model

MVP ships built-in checker strategies:

- exact match
- ignore trailing whitespace
- ignore continuous whitespace

Special Judge is a planned extension. The first version should reserve:

- `checker_kind`
- `checker_hash`
- `checker_policy`
- `checker_message`
- `checker_artifact_id`
- manifest fields for checker runtime and version

Core must distinguish user-visible safe checker summaries from internal checker logs.

## Sandbox And Security

Production default sandbox backend is Docker runner with `runsc`/gVisor. `process` backend is allowed only for local development and unit tests. Production startup must fail if configured with unsafe `process` backend or Docker without the configured gVisor runtime.

`isolate`, `nsjail`, and microVM runners remain future backends behind the same `Sandbox` interface.

Security layers:

- **Process boundary**: `soj-judge-agent` is an independent process and does not hold business database credentials.
- **Task boundary**: every attempt uses an isolated workspace or box id.
- **Execution boundary**: both compile and run happen inside sandbox; compilation is also untrusted execution.
- **Storage boundary**: testcase cache is read-only; attempt output goes to an isolated workspace.

Default restrictions:

- no network
- read-only rootfs where possible
- no Docker socket mount
- no host writable mount except the bounded attempt workspace
- no long-lived business database, Redis, or object-storage credentials inside the sandbox
- read-only testcase mount
- bounded output directory
- bounded temp disk
- bounded process count
- bounded open files
- bounded CPU time and wall time
- bounded memory

Runner must also enforce an outer watchdog so a stuck sandbox command cannot leak agent workers.

Checker and validator execution are also untrusted when they come from problem authors. MVP built-in checkers run outside the user program sandbox but inside trusted agent code. Future custom checkers and validators must run in their own sandbox profile with independent CPU, wall, memory, output and filesystem limits.

Abuse test coverage:

- infinite loop
- fork bomb
- large output
- excessive memory allocation
- illegal file access
- network access
- abnormal signal exit
- compile-time abuse
- sandbox initialization failure

## Resource Limits

Resource limits are merged from:

1. global agent defaults
2. language profile defaults
3. problem/testcase limits
4. request strategy overrides where allowed

Supported limit dimensions:

- CPU time
- wall time
- memory
- output size
- temp disk
- process count
- open file count
- compile timeout
- total attempt timeout

Verdict normalization:

- CPU or wall timeout maps to `time_limit`.
- Memory cap maps to `memory_limit`.
- Output cap maps to `output_limit`.
- Non-zero exit or signal maps to `runtime_error`, unless sandbox classified a stronger limit reason.
- Sandbox or infrastructure failures map to `system_error` with `error_class`.

## Event Contract

Events are versioned and transport-neutral. Redis Stream is the initial transport, not the domain boundary.

Event types:

```text
judge.request.v1
judge.progress.v1
judge.result.v1
judge.heartbeat.v1
judge.dead_letter.v1
```

`judge.request.v1` should include:

- `protocol_version`
- `event_id`
- `attempt_id`
- `submission_id` or `run_id`
- `trace_id`
- language id, slug and profile version
- source artifact id, storage key and content hash
- testcase set id and hash
- testcase index, group, input key, expected output key, limits and score
- aggregate resource limits
- strategy hints such as `stop_on_first_failure`, `scoring_mode`, and `checker_policy`
- priority such as `formal`, `self_run`, or `rejudge`

`judge.result.v1` should include:

- `protocol_version`
- `event_id`
- `request_event_id`
- `attempt_id`
- `trace_id`
- agent id, version, hostname and sandbox backend
- terminal status
- aggregate score, time, memory, first failed case and first failed group
- compile status, compile time, output summary and artifact id
- case-level status, score, time, memory, exit code, signal, checker message, diff summary and artifact ids
- manifest fields
- diagnostics with `error_class`, `retryable`, safe message and internal message

`judge.progress.v1` is optional for MVP UI timelines. It can update attempt lifecycle states such as `compiling`, `running`, and `checking`, but it must not write partial case results or current public projections. Terminal truth remains `judge.result.v1`.

Large payloads must go through artifact storage. Stream events carry keys, hashes, ids, summaries and structured results.

Canonical verdict names at the event, database, and API projection boundary are:

```text
accepted
wrong_answer
compile_error
runtime_error
time_limit
memory_limit
output_limit
system_error
canceled
```

Existing in-process names such as `time_limit_exceeded` and `memory_limit_exceeded` must be mapped at adapter boundaries during Phase 0. The async protocol must not introduce another enum variant set.

Required event identity fields:

- `event_id` is required on every event
- `attempt_id` is required on every request, progress, result and dead-letter event
- `request_event_id` is required on result, progress and dead-letter events
- `trace_id` is required on every production event

The current in-process `AgentRequest` shape may remain as a local adapter DTO, but production `judge.request.v1` must not mark `attempt_id` as optional and must not inline source bytes.

Production request payloads use artifact references:

- `source_artifact_id`
- `source_storage_key`
- `source_content_hash`

Local fake or unit-test runners may use inline source only in a separate test DTO or adapter path.

## Idempotency And Consistency

The attempt is the idempotency unit.

Rules:

- `judge.request.v1` must carry `attempt_id`.
- `judge.result.v1` must carry the same `attempt_id`.
- result consumer only updates rows for the matching attempt.
- repeated terminal result messages for the same attempt are idempotent.
- case results are upserted or replaced per attempt in one transaction.
- late results from old attempts cannot overwrite the current submission projection.
- rejudge creates a new attempt; it does not mutate previous attempts.
- request consumers publish `judge.result.v1` before acknowledging `judge.request.v1`; if the agent crashes between publish and ack, duplicate execution is acceptable and terminal result handling remains idempotent.
- result consumers acknowledge `judge.result.v1` only after the database transaction commits.
- progress events are best-effort and monotonic per attempt; stale progress cannot move a terminal attempt backward.
- consumer claiming requires a lease or fencing token so two agents cannot both be considered the active executor for the same attempt.
- result consumer deduplicates by `(attempt_id, request_event_id)` and rejects mismatched attempt/request pairs.

`submission_results` remains a current projection. `judge_attempts` and `judge_case_results` remain the durable evidence source.

## Outbox And Inbox Consistency

Stream publication and database state changes need explicit recovery rules.

MVP should use a transactional outbox for request events:

- dispatcher claims or creates work inside a database transaction
- dispatcher writes an outbound request event row with stable `event_id`
- dispatcher commits the transaction
- publisher sends the event to Redis Stream
- publisher marks the outbox row as published after Redis confirms

If Redis publish succeeds but marking the outbox row fails, retrying publish with the same `event_id` is safe. Agent and result consumer deduplicate by event identity.

Result handling should use an inbox or processed-event table:

- result consumer records `event_id`, `attempt_id`, and `request_event_id`
- result consumer persists attempt/case/projection updates in the same transaction
- ack happens only after commit

This avoids the current failure mode where stream publish succeeds but task dispatch state fails to update.

## Priority And Slot Scheduling

MVP does not include a standalone Scheduler service, but it still needs deterministic priority behavior.

Initial Redis Stream layout:

```text
judge.request.formal
judge.request.self_run
judge.request.rejudge
judge.result
judge.dead_letter
```

Consumers poll with weighted priority:

- formal submissions first
- self-run second
- rejudge only when capacity remains

Slot scopes:

- **per-agent global slots** cap local sandbox concurrency
- **per-agent language slots** prevent one language from starving others on the same agent
- **cluster-wide caps** are not enforced in MVP without Scheduler; they are approximated by deployment sizing and per-agent config

When formal queue depth or oldest pending age crosses thresholds, agents stop taking self-run and rejudge work until the formal queue recovers.

## Retry And Dead Letter

Retry classification:

- Retryable: Redis transient failure, object storage transient failure, agent crash, sandbox temporary initialization failure.
- Non-retryable: disabled language, missing profile, testcase hash mismatch, incompatible protocol version, invalid request.

Agent health behavior:

- unhealthy agents stop claiming new tasks
- running tasks complete or timeout
- pending tasks can be claimed by another consumer after lease timeout
- repeated system errors can trip a language or sandbox circuit breaker

Dead-letter messages should preserve:

- attempt id
- request event id
- trace id
- error class
- retry count
- last safe message
- last internal diagnostic reference

## Testcase Cache

Agent uses local read-only testcase cache keyed by `testcase_set_hash`.

Cache behavior:

- verify hash before use
- never mutate cached testcase files during execution
- mount or copy cache into sandbox read-only
- write outputs only into attempt workspace
- delete and re-fetch corrupted cache entries
- evict with capacity limit and LRU policy
- use per-agent singleflight to prevent multiple local workers from downloading the same testcase set at once
- expose cache capacity, eviction count, cold fetch duration and corrupted-entry count as metrics

User program sandboxes must receive testcase input only. Expected output files are not mounted into the user program sandbox. Built-in checkers run outside the user program sandbox with read access to expected output and actual output; future Special Judge checkers must run in a separate checker sandbox profile.

Future contest warmup can prefetch testcase sets before contest start, but it is not required for MVP.

## Artifact Strategy

MVP should keep Stream payloads small and use artifact refs for large data:

- source
- compile output
- stdout
- stderr
- output diff
- judge log
- manifest snapshot where needed

Safe summaries are stored on the result rows and exposed by visibility policy. Raw logs and full outputs are admin-only unless later policy allows otherwise.

## Observability

Metrics should cover:

- request stream depth
- result stream depth
- oldest pending age
- stream event size and rejected oversized events
- stream retention trimming and dead-letter counts
- consumer lag
- pending claim count
- agent slot usage
- language slot usage
- sandbox prepare/compile/run/check duration
- verdict counts by language
- system error counts by class
- testcase cache hit rate
- artifact write size and latency
- result persist latency

Every request/result should carry `trace_id` so API, dispatcher, agent, result consumer and database evidence can be correlated.

Redis Stream operational limits:

- keep request/result events under a configured size budget
- use approximate `MAXLEN` or retention policy for high-volume streams
- monitor `XPENDING` and use `XAUTOCLAIM` or equivalent recovery for abandoned work
- stop dispatching or lower non-formal priority when queue depth or oldest pending age crosses configured thresholds
- keep dead-letter streams bounded and searchable by attempt id and trace id

## Performance Strategy

MVP throughput depends on sandbox slots and average attempt duration:

```text
submissions per minute ~= agent_count * sandbox_slots_per_agent * 60 / avg_attempt_seconds
```

Example:

- 1 agent, 8 slots, 2 seconds average attempt time: about 240 submissions/minute theoretical.
- 3 agents under similar workload: about 700 submissions/minute theoretical.

Actual throughput will be lower under CE-heavy, TLE-heavy, many-case, or slow-language workloads. Therefore:

- enforce global agent slots
- enforce per-language slots
- prioritize formal submissions over self-run and rejudge
- support early stop where scoring policy allows
- keep Redis Stream payloads small
- cache testcase sets locally
- observe P95/P99 compile, run and check duration

## Visibility Boundary

Judge Core always returns the most complete normalized result it can produce. It does not decide what a contestant, problem author, contest owner, admin, or root user can see.

Visibility remains above Core:

```text
judgecore full evidence
  -> result consumer persists internal facts
  -> submission service projects by actor and contest state
  -> API returns safe shape
```

ACM mode is a projection and scoring policy concern, not a reduced core result model.

## Delivery Phases

### Phase 0: Protocol And Event Skeleton

Deliverables:

- current worker compatibility split: dispatcher, fake agent and result consumer
- durable attempt prerequisite check
- `judge.request.v1`
- `judge.progress.v1`
- `judge.result.v1`
- `judge.heartbeat.v1`
- `judge.dead_letter.v1`
- Redis Stream adapter
- transactional outbox for request events
- result inbox or processed-event table
- canonical verdict enum mapper
- result consumer skeleton
- fake agent loop

Acceptance:

- fake agent consumes request and publishes result
- result consumer persists terminal attempt transactionally
- duplicate result messages are idempotent
- request ack happens only after result publish, and result ack happens only after transaction commit
- `time_limit_exceeded` and `memory_limit_exceeded` are normalized to canonical event/database statuses
- production request events require `attempt_id`, `event_id`, `request_event_id` where applicable, and artifact refs instead of inline source
- Redis publish and database dispatch state can be retried safely without losing or duplicating terminal results

### Phase 1: Judge Agent Process

Deliverables:

- `cmd/soj-judge-agent`
- health endpoint
- graceful shutdown
- consumer group handling
- global slots and language slots
- agent id/version/heartbeat

Acceptance:

- Docker Compose starts agent
- agent can restart and continue pending work
- agent does not connect to business database

### Phase 2: Judge Core Pipeline

Deliverables:

- core pipeline orchestrator
- language registry
- Go and C++17 profiles
- built-in checkers
- testcase cache
- artifact integration
- manifest builder

Acceptance:

- real Go and C++ submissions produce AC, WA, CE, RE and TLE
- case-level results persist correctly
- manifest identifies language runtime, sandbox profile, testcase hash and judge core version
- Python is not part of MVP acceptance unless the implementation plan explicitly expands language scope

### Phase 3: Sandbox Hardening

Deliverables:

- Docker runner sandbox adapter with gVisor/runsc production gate
- process backend restricted to dev/test
- startup capability probe for Docker, runner images, and runsc
- resource limit mapping
- watchdog
- abuse regression tests

Acceptance:

- production agent refuses to start if configured sandbox backend is unsafe or Docker/runsc capabilities are missing
- infinite loop maps to `time_limit`
- memory abuse maps to `memory_limit`
- large output maps to `output_limit`
- illegal file or network access does not escape sandbox
- hidden expected output is never visible inside the user program sandbox
- sandbox failures map to `system_error` with error class

### Phase 4: Performance And Operations

Deliverables:

- testcase cache LRU and metrics
- per-agent testcase download singleflight
- queue and agent Prometheus metrics
- priority and slots enforcement
- local pressure script
- Docker smoke covering full async flow

Acceptance:

- local Docker full flow passes
- smoke creates submission, agent judges it, result consumer persists it, API returns safe projection
- pressure run shows queue drains and cache hit rate improves after warm cache

## Parallel Work Units

Parallelizable after Phase 0 contract is stable:

- Event protocol and Redis Stream integration.
- Judge agent process and lifecycle.
- Judge Core pipeline, language profiles and checker.
- Sandbox adapter and resource-limit tests.
- Docker, smoke scripts, metrics and documentation.

Serial dependencies:

- Event contract must precede agent/result-consumer implementation.
- Core interfaces must precede language profile and sandbox adapter deepening.
- Result consumer persistence must follow the already-completed durable judge data model.

## Open Questions For Implementation Plan

- Whether `result-consumer` lives inside `soj-worker` initially or becomes a separate command.
- Whether Docker plus gVisor/runsc is available in the target environment without extra host setup.
- Whether source artifacts should be fetched by agent directly or materialized into request-local refs by dispatcher.
- Whether first pressure test should target single-machine Docker only or multi-agent local compose.
- Which exact status enum names should be normalized before implementation to avoid existing `time_limit` vs `time_limit_exceeded` drift.
