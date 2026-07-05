# SOJ Docker gVisor Runner Implementation Plan

日期：2026-07-05

## Scope

按 `2026-07-05-soj-docker-gvisor-runner-design.md` 实现生产主线 sandbox：Docker runner + gVisor/runsc。计划保持阶段边界清晰，不拆到逐函数级别。

## Phase 0: Spec And Compatibility Baseline

目标：

- 确认 Docker gVisor runner design 与现有 v2/JudgeCore spec 对齐。
- 更新文档中 production sandbox 默认方向，避免 isolate 与 docker-gVisor 表述冲突。
- 保持现有 fake/process smoke 可运行。

验收：

- spec/plan committed。
- `go test ./...`、`go vet ./...`、Compose config 通过。

## Phase 1: Backend-Neutral Sandbox Abstraction And Slots

目标：

- 演进 `internal/judgecore/sandbox` 顶层契约，隔离 JudgeCore 与 Docker 细节。
- 引入 backend capability probe 和 production safety gate。
- 为 judge-agent 增加 global slots 和 per-language slots。

验收：

- `process` 和 `fake` 现有路径继续通过。
- judge-agent 可配置 `SOJ_JUDGE_PARALLELISM`。
- 单元测试覆盖 slot 获取、释放、取消和语言限流。

## Phase 2: Docker Runner Backend

目标：

- 实现 `SOJ_JUDGE_SANDBOX_BACKEND=docker`。
- 增加 Docker client wrapper，便于测试替换。
- 支持 Go/C++17 compile/run/case output collection。
- 实现安全基线：network none、read-only、cap drop、no-new-privileges、non-root、CPU/memory/pids/output/time limits。

验收：

- Docker backend 可跑 Go/C++17 AC、WA、CE、RE、TLE、OLE。
- runner 容器拿不到业务凭证和 expected output。
- workspace cleanup 幂等。

## Phase 3: Runner Images And Local Tooling

目标：

- 增加 Go/C++17 runner image 构建。
- 增加本地 Docker/gVisor 安装和检查脚本。
- 增加 `make runner-images`、`make smoke-real-docker`、`make smoke-real-gvisor`。

验收：

- 本地普通 Docker backend smoke 通过。
- 本地 runsc/gVisor backend smoke 在组件可用时通过。
- 组件不可用时脚本输出明确原因。

## Phase 4: Production gVisor Gate And Deploy Docs

目标：

- `SOJ_ENV=prod` 强制 Docker runtime 使用 `runsc`。
- 更新 Compose/env/example/deploy docs。
- 记录单机和多 judge 节点部署方式。

验收：

- prod 未配置 runsc 时 judge-agent 启动失败且错误明确。
- prod 配置 runsc 时 readiness 通过。
- 文档说明 Docker daemon 权限边界和 judge 节点隔离要求。

## Phase 5: Capacity Smoke And Observability

目标：

- 增加 slots benchmark/smoke。
- 输出 submissions/minute、P95/P99 attempt latency、container startup overhead、memory curve、queue oldest pending age。
- 强化 Prometheus 指标：slot usage、Docker phase duration、backend errors、cleanup failures。

验收：

- 跑 `1/2/4/8/16` slot benchmark。
- 记录是否达到数百提交/分钟级别；未达到时给出瓶颈和下一步优化。
- CI 保留轻量验证，本地或手动 workflow 跑完整 Docker/gVisor 验证。

## Commit Strategy

- Phase 0: `docs(judge): design docker gvisor runner`
- Phase 1: `feat(judge-agent): add sandbox slots and backend capabilities`
- Phase 2: `feat(judgecore): add docker runner backend`
- Phase 3: `feat(deploy): add runner images and local gvisor tooling`
- Phase 4: `docs(deploy): document docker gvisor production path`
- Phase 5: `test(judge): add runner capacity smoke`
