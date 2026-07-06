# SOJ Judge Runtime Production Readiness Design

日期：2026-07-06

## Decision

本阶段把已经能本地真实评测的 Docker/gVisor runner 主线，推进到可以认真部署试运行的状态。范围锁定为三类能力：readiness、dead-letter/恢复和关键指标。

这个阶段不继续扩展评测语言、不做新的调度服务，也不把 scoreboard snapshot、OpenTelemetry tracing 或前端后台纳入同一批改动。

## Goals

- Worker 和 judge-agent 的 `/readyz` 能反映关键依赖，而不是只证明进程还活着。
- 已 dead-letter 或卡住的 judge task 有可记录、可重复执行的恢复路径。
- 评测运行时暴露足够的 Prometheus 指标，用于判断队列、恢复、sandbox 和 agent 容量是否健康。
- 输出可挂到 README 和部署文档的验证文档，包含本地验证环境、测试命令、测试结果和排障入口。

## Non-Goals

- 不实现新的比赛 scoreboard 自动快照。
- 不接入 OpenTelemetry tracing。
- 不实现多机 scheduler 或 runner warm pool。
- 不改变 judge request/result 协议版本。
- 不把普通 Docker runtime 视为生产隔离方案。

## Readiness

API readiness 已检查 PostgreSQL，本阶段补齐 worker 和 judge-agent：

- `soj-worker` readiness 检查 PostgreSQL、Redis request stream、Redis result stream 和对象存储可用性。
- `soj-judge-agent` readiness 检查 Redis request stream、Redis result stream、对象存储和 sandbox backend capability probe。
- readiness 失败继续返回 HTTP 503，错误细节只进入日志或内部指标，避免把凭据、DSN 或内部地址暴露给普通 HTTP 响应。
- 检查函数保持小而可单测，运行时入口只负责组装依赖。

## Dead-Letter And Recovery

现有 worker 已能在任务耗尽重试后写 PostgreSQL `dead` 状态和 Redis dead-letter stream。缺口是运维恢复入口和恢复结果观测。

本阶段新增一个最小 CLI/命令模式，用于把指定 dead task 恢复为 pending：

- 输入 task id 和可选 reason。
- 只允许恢复 PostgreSQL 中仍处于 `dead` 的 judge task。
- 恢复时把 task 状态改回 `pending`、`next_run_at=now()`，递增或保留 attempts 的行为必须明确记录。
- 对应 submission 从 `system_error` 恢复为 `queued`。
- 命令输出恢复数量和未恢复原因，便于贴到事故记录。

Redis dead-letter stream 保留为诊断证据，不在第一版做自动回放所有 dead-letter 消息，避免绕过 PostgreSQL 事实源。

## Metrics

新增或强化以下指标：

- readiness dependency check total/duration by service、dependency、result。
- worker recovery total by action/result。
- reconciler reset/stale-run total by action/result。
- judge-agent readiness sandbox probe result。

保留现有指标：

- `soj_worker_judge_task_dispatch_total`
- `soj_worker_judge_tasks_total`
- `soj_worker_judge_task_duration_seconds`
- `soj_judge_agent_slots_used`
- `soj_judge_agent_slots_capacity`
- `soj_sandbox_phase_duration_seconds`
- `soj_sandbox_backend_errors_total`
- `soj_sandbox_cleanup_failures_total`

## Documentation Output

完成阶段时更新：

- `docs/judge-runtime-readiness.md`：readiness 检查、恢复操作、关键指标、排障步骤、本地验证环境。
- `README.md` 和 `README.zh-CN.md`：增加指向 readiness 文档的链接。
- `docs/v2-deploy.md`：把 worker/judge-agent readiness 从“待扩展”更新为当前生产试运行检查项。

## Acceptance

- 单元测试覆盖 readiness dependency failures、dead task recovery 和新增指标注册。
- `go test ./...` 通过。
- `go vet ./...` 通过。
- `docker compose -f deploy/docker-compose.yaml config` 通过。
- `make smoke-real-docker` 或等价真实 Docker runner smoke 通过。
