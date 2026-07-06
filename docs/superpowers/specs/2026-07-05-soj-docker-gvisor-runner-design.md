# SOJ Docker gVisor Runner Design

日期：2026-07-05

## Decision

SOJ production sandbox 主线调整为 **Docker runner + gVisor/runsc**。

Docker 负责 runner 容器编排、镜像分发、资源参数和本地运维体验；gVisor `runsc` 负责生产隔离边界。SOJ 不把普通 Docker 容器视为生产安全沙箱，也不在这一阶段自研 Linux namespace/cgroup/seccomp 底层 runner。

现有 JudgeCore、judge-agent、Redis Stream、attempt evidence 和 result consumer 边界保持不变。新增工作集中在 `internal/judgecore/sandbox` 的可替换 backend 抽象、judge-agent 并发槽位、Docker runner backend、gVisor 生产校验、runner images 和本地真实评测验证。

## Goals

- 支撑既有目标规模：中型公开 OJ、`10 万级用户`、`1k-5k` 并发用户、峰值数百次提交/分钟。
- 保持 sandbox 顶层抽象稳定，后续可替换为 go-judge、nsjail、isolate、Kata 或 Firecracker。
- 单机可运行：本地或单台服务器可以安装 Docker + runsc 后跑完整真实判题。
- 支持多机扩容：多个 judge-agent 节点共享 Redis request/result streams 和对象存储。
- 支持 Go 和 C++17 首批真实代码执行，并覆盖 AC、WA、CE、RE、TLE、MLE、OLE、system_error。
- 生产环境强制 gVisor；开发环境可以显式允许普通 Docker runtime 以便本地调试。
- 不把业务数据库、Redis、MinIO/S3 凭证暴露给用户代码 runner 容器。

## Non-Goals

- 不在第一版自研 seccomp/cgroup namespace runner。
- 不在第一版实现 Firecracker/Kata microVM runner。
- 不实现用户自定义 Special Judge 的 sandbox 执行。
- 不让 API 或 worker 直接调用 Docker。
- 不把 Docker socket 暴露给 runner 容器。
- 不把 testcase expected output 挂载给用户程序容器。

## Architecture

目标形态：

```text
soj-api
  -> create submission / query projection

soj-worker
  -> dispatch judge.request.v1
  -> consume judge.result.v1
  -> persist attempt/case/projection/contest transactionally

soj-judge-agent
  -> consume judge.request.v1 with bounded slots
  -> JudgeCore
       -> SandboxBackend interface
            -> process backend       dev/test only
            -> docker backend        local/prod
            -> future backends       go-judge/nsjail/isolate/firecracker
  -> publish judge.result.v1

Docker Engine on judge node
  -> runsc/gVisor runtime in production
  -> per-language runner images
```

`soj-judge-agent` 是唯一允许控制 Docker Engine 的 SOJ 进程。它必须运行在专用 judge 节点上；runner 容器只接收源码、输入、编译产物和输出目录，不接收业务服务凭证。

## Top-Level Sandbox Contract

JudgeCore 只能依赖稳定的 sandbox 能力契约，不依赖 Docker 概念。现有 `Sandbox` 接口需要演进为 backend-neutral contract：

```go
type Backend interface {
    Name() string
    Profile() string
    Probe(ctx context.Context) (Capabilities, error)
    Prepare(ctx context.Context, request PrepareRequest) (AttemptWorkspace, error)
    Compile(ctx context.Context, workspace AttemptWorkspace, request CompileRequest) (CompileResult, error)
    RunCase(ctx context.Context, workspace AttemptWorkspace, request RunCaseRequest) (RunResult, error)
    Cleanup(ctx context.Context, workspace AttemptWorkspace) error
}
```

关键要求：

- `Probe` 返回 production safety、runtime、runner image、cgroup/memory/pids/network capability。
- `Prepare` 创建 attempt-scoped workspace，不暴露 hidden expected output 给用户容器。
- `Compile` 和 `RunCase` 都只返回 normalized sandbox result，不泄漏 Docker 内部错误给普通用户。
- `Cleanup` 必须幂等，失败时返回可观测的 internal diagnostic。
- backend manifest 必须写入 `sandbox_backend`、`sandbox_profile`、runtime 信息和 capability 摘要。

顶层抽象必须允许这些 backend 共存：

| Backend | 用途 | 生产默认 |
| --- | --- | --- |
| `process` | dev/test/local fallback | 否 |
| `docker` | Docker runner + optional gVisor | 是 |
| `go-judge` | future external sandbox service | 否 |
| `nsjail` | future host sandbox adapter | 否 |
| `isolate` | future host sandbox adapter | 否 |
| `firecracker` | future microVM adapter | 否 |

## Docker Runner Backend

Docker backend 使用 per-language runner image：

- `soj-runner-go`
- `soj-runner-cpp17`

第一版 runner lifecycle：

1. `Prepare` 在 host 创建 attempt workspace。
2. `Compile` 启动一次 compile container，将源码目录挂载到 `/workspace`，输出 binary 到 attempt workspace。
3. `RunCase` 为每个 testcase 启动 run container，挂载 binary/source 只读，挂载 case input 只读，挂载 case output 目录可写。
4. builtin checker 在用户程序容器外运行，只读取 expected output 和 actual output。
5. `Cleanup` 删除 workspace 并清理遗留容器。

安全基线：

- `NetworkDisabled=true`
- root filesystem read-only
- writable path 只允许 `/workspace` 或 per-case output tmpfs/bind mount
- `CapDrop=["ALL"]`
- `SecurityOpt=["no-new-privileges"]`
- 非 root 用户运行用户程序
- memory limit、CPU quota、pids limit、file/output limit、wall timeout
- 禁止 bind mount Docker socket
- 禁止挂载业务配置、业务凭证和 host sensitive paths
- runner container labels 包含 attempt id、trace id、language、phase，便于清理和审计

生产环境必须使用 Docker runtime `runsc`。开发环境允许普通 Docker runtime，但必须显式设置 `SOJ_DOCKER_RUNNER_ALLOW_UNSAFE=1` 或 `SOJ_ENV=local/dev/test`。

## gVisor Production Gate

`SOJ_ENV=prod` 且 `SOJ_JUDGE_SANDBOX_BACKEND=docker` 时，judge-agent 启动必须校验：

- Docker daemon 可访问。
- Docker server version 满足最低版本要求。
- Docker runtime 列表包含配置的 gVisor runtime，默认 `runsc`。
- `soj-runner-go` 和 `soj-runner-cpp17` 镜像存在或可拉取。
- backend probe 能启动一个 no-op runsc container 并确认 network disabled、read-only rootfs 和 non-root user 生效。

任一校验失败时拒绝启动，并返回明确错误。生产不得自动降级到 `process` 或普通 Docker runtime。

## Local Setup

仓库应提供本地安装和校验脚本：

- `scripts/dev/install-gvisor.sh`：按官方方式安装 `runsc` 并注册 Docker runtime。
- `scripts/dev/check-docker-runner.sh`：校验 Docker、runsc、runner images 和最小 no-op container。
- `make runner-images-pull`：拉取已发布的 Go/C++17 runner images。
- `make runner-images-build`：仅在开发 Dockerfile 时本地构建 Go/C++17 runner images。
- `make smoke-real-docker`：使用 Docker backend 跑真实判题 smoke。
- `make smoke-real-gvisor`：使用 Docker backend + runsc 跑真实判题 smoke。

本地 WSL2 或非标准 Linux 环境属于 best-effort。脚本必须把不可用原因说清楚，而不是静默降级。

## Agent Concurrency

为达到峰值数百提交/分钟，judge-agent 必须引入 bounded slots：

- `SOJ_JUDGE_PARALLELISM`：agent 全局并发槽位。
- `SOJ_JUDGE_LANGUAGE_SLOTS`：按语言限制，例如 `go=4,cpp17=4`。
- `SOJ_JUDGE_MAX_BATCH`：每轮最多从 Redis Stream 读取的消息数，不得超过可用 slot。

处理规则：

- 消费 Redis 前先计算可用 slot，避免一次读取大量消息后长时间 pending。
- 每条消息占用一个 global slot 和一个 language slot。
- 结果发布成功并 ack 后释放 slot。
- shutdown 时停止读取新消息，等待已占用 slot 在 shutdown timeout 内完成或取消。
- formal submission 优先；self-run 和 rejudge 后续进入低优先级队列或在容量不足时降级。

## Capacity Model

沿用 Judge Core MVP 公式：

```text
submissions per minute ~= agent_count * sandbox_slots_per_agent * 60 / avg_attempt_seconds
```

设计目标：

- 1 个 agent、8 slots、平均 2 秒 attempt：理论约 240 submissions/minute。
- 3 个 agent、类似 workload：理论约 700 submissions/minute。
- CE-heavy、TLE-heavy、many-case、slow-language workload 必须按压测结果折减。

Docker/gVisor runner 的生产实现必须避免把每个 testcase 都变成不可控冷启动成本。第一版可以一 case 一 run container，并通过 sandbox phase metrics 观察 container startup overhead。

## Observability

新增或强化指标：

- agent global slot usage
- per-language slot usage
- Docker container create/start/wait/remove duration
- gVisor runtime selected count and unsafe runtime rejection count
- compile/run/check duration P50/P95/P99
- container startup overhead P50/P95/P99
- verdict counts by language/backend
- backend system error counts by class
- workspace cleanup failures
- local testcase cache hit/miss/corruption/eviction
- queue oldest pending age and consumer lag

所有 Docker labels、logs 和 metrics 都必须携带 attempt id、task id、trace id、language 和 phase。

## Test And Acceptance

Functional acceptance:

- Go/C++17 AC、WA、CE、RE、TLE、OLE 通过 Docker backend。
- `SOJ_ENV=prod` 未配置 runsc 时 judge-agent 拒绝启动。
- `SOJ_ENV=prod` 配置 runsc 后 Docker backend smoke 通过。
- runner 容器内无法访问网络。
- runner 容器无法读取业务凭证。
- hidden expected output 不进入用户程序容器。
- duplicate Redis messages 不重复写 terminal projection。

## Risks

- **Docker daemon 是高权限边界。** Mitigation: judge-agent 独占专用 judge 节点，runner 容器不接触 Docker socket。
- **gVisor 兼容性可能影响语言 runtime。** Mitigation: 首批只支持 Go/C++17，runner images 和 smoke 覆盖常见系统调用。
- **容器冷启动影响峰值吞吐。** Mitigation: slots、testcase cache 和 startup overhead 指标。
- **本地环境差异大。** Mitigation: local check script 明确报告 Docker/runsc/cgroup/WSL2 限制。
- **抽象过早绑定 Docker。** Mitigation: JudgeCore 只依赖 backend-neutral contract，Docker API 封装在 docker backend 内部。

## Migration Notes

现有文档中 “production default sandbox backend is isolate” 的描述需要在实现阶段更新为 “production default sandbox backend is docker with runsc/gVisor”。`isolate` 保留为 future host sandbox backend，而不是当前主线。
