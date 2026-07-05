# SOJ Engineering Mainline Priorities Design

## Decision

SOJ 下一阶段采用 **工程主线优先**：先把项目从已经成型的 v2 后端和 fake judge 流程，推进到真实、安全、可验证、可部署的 Online Judge 后端。

推荐路线是 **真实评测主线优先，同时补最小 CI 护栏**。CI/CD 不单独成为长期独立阶段，而是作为每个真实评测里程碑的进入条件和回归保障。

## Why This Matters

SOJ 当前已经具备清晰的 v2 后端结构：

- `soj-api` 提供 REST API。
- `soj-worker` 负责评测任务分发和结果消费。
- `soj-judge-agent` 已作为独立进程存在。
- PostgreSQL 是事实源，Redis Stream 用于异步投递。
- JudgeCore、语言 profile、checker、sandbox adapter 已有初步模块边界。
- Docker Compose 可以跑通 fake judge smoke flow。

下一步的核心问题不是继续扩展外围功能，而是回答一个 OJ 项目最关键的问题：

> SOJ 能不能真实、安全、可重复地完成一次评测，并且每次代码改动都能被自动化验证？

因此，最高优先级应放在真实 judge sandbox、真实语言执行、结果可解释性、全链路 smoke 和最小工程护栏上。

## Goals

- 让本地 Docker 环境可以跑通真实 Go/C++17 提交，而不只依赖 `fake://accepted`。
- 让 `soj-judge-agent` 具备生产形态的 sandbox backend，优先支持 `isolate`。
- 让一次评测结果具备可解释、可复现、可审计的基础信息。
- 让每个核心改动都能通过 GitHub Actions 和本地测试被验证。
- 明确 unsafe backend、secret、metrics 和 judge-agent 权限边界，降低后续部署风险。

## Non-Goals

本阶段不优先做以下事项：

- 不建设完整前端或管理员后台。
- 不扩展大量新语言；MVP 聚焦 Go 和 C++17。
- 不实现完整独立 scheduler 服务。
- 不实现用户自定义 Special Judge 的 sandbox 执行。
- 不做复杂多集群调度、冷热数据分区或平台级运维系统。
- 不把 product polish 放在真实评测能力之前。

## Candidate Approaches Considered

### Approach A: Real Judge Mainline First

优先完成真实评测能力：`isolate` sandbox、Go/C++17 profile、资源限制、结果归一、case-level result、真实 Docker smoke。

优点：

- 最贴近 OJ 核心价值。
- 后续前端、管理员后台、比赛能力都有真实后端可接。
- 能尽早暴露安全、资源限制、语言 profile、testcase I/O 等根问题。

代价：

- 工程难度最高。
- 需要认真处理 sandbox 安全、测试稳定性和 Docker 环境差异。

### Approach B: CI/CD And Deployment First

优先补 GitHub Actions、PR 保护、Docker build、Compose 校验、OpenAPI 校验、部署文档和 secret 策略。

优点：

- 立刻降低协作和发布风险。
- 对所有后续改动都有帮助。

代价：

- 核心评测能力仍然依赖 fake path。
- 项目价值不会明显跃迁。

### Approach C: Operations Stability First

优先做队列可观测性、dead-letter 操作、重试/重判、告警指标、worker readiness 和故障恢复演练。

优点：

- 能让现有 async 架构更可靠。
- 对比赛峰值场景有长期价值。

代价：

- 没有真实 sandbox 前，很多稳定性验证仍然偏空转。
- 容易优化尚未承载真实负载的路径。

## Recommendation

选择 **Approach A: Real Judge Mainline First**，但把 Approach B 的最小部分作为 P0 护栏一起落地。

排序原则：

1. 能证明 SOJ 真实判题的事项优先。
2. 能阻止评测主线回归的自动化优先。
3. 能解释和复现判题结果的证据优先。
4. 能降低部署安全风险的边界优先。
5. 产品体验、后台、更多语言和高级运维能力后置。

## Priority Plan

### P0: Minimal Engineering Guardrails

目标：后续评测主线改动必须有基础自动化兜底。

交付内容：

- GitHub Actions workflow。
- `go test ./...`。
- `go vet ./...`。
- `docker compose -f deploy/docker-compose.yaml config`。
- Docker build for `soj-api`、`soj-worker`、`soj-judge-agent`、`soj-migrate`。
- 保留本地 smoke test 作为手动或后续 CI 扩展目标。

验收标准：

- PR 或 main push 能自动跑基础检查。
- 检查失败时阻止继续合并或至少给出明确失败信号。
- workflow 不依赖本地开发机上的私有状态。

### P1: Real Judge Sandbox MVP

目标：`soj-judge-agent` 可以通过生产形态 sandbox 执行真实代码。

交付内容：

- `isolate` sandbox backend 接入现有 `internal/judgecore/sandbox` 边界。
- Go 和 C++17 语言 profile。
- 编译阶段和运行阶段都走 sandbox。
- CPU、wall time、memory、output size、process count、file descriptor 等基础限制。
- 无网络、受限 workspace、只读 testcase 访问策略。
- unsafe `process` backend 在非 dev/test/local 环境下启动失败。

验收标准：

- Go/C++17 accepted 程序可以被真实执行并返回 AC。
- 编译错误返回 CE。
- 无限循环返回 TLE。
- 超内存返回 MLE 或稳定归一到对应平台错误。
- 大输出返回 OLE。
- 异常退出返回 RE。
- sandbox adapter 单元测试覆盖主要 verdict normalization。

### P2: Trustworthy Judge Results

目标：评测结果不仅有最终状态，还能解释、复现和审计。

交付内容：

- case-level result 持久化和安全投影核对。
- compile output 摘要。
- first failed case 或 group。
- checker message 的安全摘要。
- runtime fingerprint。
- judge manifest，包含 judge core version、sandbox backend/profile、language runtime、testcase set hash、checker kind/hash、resource limits、attempt id、trace/request id。

验收标准：

- API 对普通用户只暴露安全字段。
- 管理员或允许角色可以看到内部诊断摘要。
- 同一次 attempt 的结果和 case rows 幂等写入。
- late result 不覆盖更新的 submission projection。

### P3: Real Docker End-To-End Smoke

目标：本地 Compose 可以证明真实评测链路完成闭环。

交付内容：

- Docker image 包含或能安装真实 judge runtime 所需依赖。
- `deploy/docker-compose.yaml` 支持真实 sandbox profile。
- `deploy/smoke.sh` 增加真实 Go/C++17 提交流程，覆盖 AC 和至少一个失败 verdict。
- 保留 fake path，作为快速开发和确定性测试路径。

验收标准：

- 从 clean volumes 启动 Compose 后，真实提交可完成：
  `submit -> dispatcher -> judge-agent -> result consumer -> PostgreSQL -> API projection`。
- smoke test 能验证 Redis result stream 和数据库 terminal state。
- Prometheus metrics 能看到 API、worker、judge-agent 基础指标。

### P4: Production Deployment Closure

目标：真实评测能力可以被安全地部署和排障。

交付内容：

- 更新 `docs/v2-deploy.md` 的 production judge-agent 部署说明。
- 明确 secret 配置、object storage credential、JWT secret、Redis/PostgreSQL DSN 的处理方式。
- 明确 judge-agent 不持有业务数据库凭据。
- 明确 `/metrics` 的网络暴露策略。
- 增加 sandbox backend 安全矩阵：`fake`、`process`、`isolate` 的允许环境。
- 增加基础故障排查文档：队列堆积、result stream 无结果、agent 启动失败、sandbox verdict 异常。

验收标准：

- 新贡献者能根据文档区分 local fake flow 和 production-like real judge flow。
- 不安全 backend 不会被误用于生产环境。
- 部署文档和 README 不互相矛盾。

## Architecture Boundaries

### API Boundary

`soj-api` 负责提交创建、结果查询和安全投影。API 不直接执行判题，也不等待长耗时 judge pipeline。

### Worker Boundary

`soj-worker` 负责：

- 从 PostgreSQL claim pending judge task。
- 创建或引用 durable attempt。
- 发布 `judge.request.v1`。
- 消费 `judge.result.v1`。
- 事务性写入 attempt、case results、submission projection 和 contest projection。

### Judge Agent Boundary

`soj-judge-agent` 负责：

- 消费 judge request。
- 获取 source、testcase metadata 和 language profile。
- 调用 JudgeCore 执行 compile/run/check。
- 发布 normalized judge result。

Judge agent 不直接写业务数据库。

### JudgeCore Boundary

`internal/judgecore` 负责 OJ 领域执行语义：

- language profiles。
- sandbox adapter。
- compile/run/check pipeline。
- checker。
- result normalization。
- manifest。

JudgeCore 不依赖 Gin、HTTP handler、submission service 或 PostgreSQL repository。

## Testing Strategy

- 单元测试：verdict normalization、checker、language profile、sandbox adapter。
- 集成测试：judge-agent fake path、Redis Stream request/result flow、result consumer idempotency。
- Docker smoke：clean volumes 下跑通真实 Go/C++17 提交。
- 安全/滥用测试：infinite loop、large output、memory exhaustion、abnormal exit、file/network access attempt。
- CI 基础检查：`go test ./...`、`go vet ./...`、Compose config、Docker build。

## Risks And Mitigations

- **Risk: isolate 在本地或 CI 环境不可用。**  
  Mitigation: 保留 fake backend 和 dev-only process backend；真实 sandbox smoke 可以先作为 local/manual 或特定 runner job。

- **Risk: Docker 内运行 sandbox 需要额外权限。**  
  Mitigation: 明确 production-like profile 所需 capabilities，文档化安全权衡；默认本地 smoke 仍可跑 fake path。

- **Risk: verdict 在不同 runtime 环境下不稳定。**  
  Mitigation: 将 adapter-specific exit/status 归一集中在 sandbox/result normalizer，并用表驱动测试覆盖。

- **Risk: 真实评测主线过大。**  
  Mitigation: 按 P0-P4 拆分，每个阶段都必须有独立验收标准，不把前端、后台和高级调度混入本阶段。

## Success Criteria

本阶段完成后，SOJ 应达到：

- CI 能挡住基础 Go、Compose 和 Docker 回归。
- 本地可以通过 Docker Compose 跑通真实 Go/C++17 判题。
- judge-agent 通过 sandbox backend 执行代码，而不是只返回 fake accepted。
- 常见 verdict 有稳定归一和测试覆盖。
- 判题结果有 case-level 证据和可复现 manifest。
- 部署文档明确区分 local、dev/test 和 production-like judge 配置。

## Follow-Up After This Design

确认本设计后，下一步应进入 implementation planning，把 P0-P4 拆成可执行任务。推荐第一个实施计划聚焦：

- P0 minimal CI。
- P1 real judge sandbox MVP。
- P3 中最小真实 Docker smoke。

P2 和 P4 可以跟随 P1/P3 分阶段补齐，避免一次计划过大。
