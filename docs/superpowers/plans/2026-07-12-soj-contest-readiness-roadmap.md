# SOJ Contest Readiness Roadmap

**Goal:** 把 SOJ 从“可以完成比赛流程”推进到“可以可靠承办正式 ACM 比赛”，优先保证评测正确、比赛公平、故障可恢复和运营可控。

**Decision rule:** 不按 v1 功能逐项恢复。只有直接服务于评测、出题、参赛、比赛运营或公开部署安全的能力才进入主线。

**Progress:** Phase 1 Rejudge Recovery implemented on 2026-07-12. The next planned phase is Contest Integrity.

---

## Current Baseline

当前 `main` 已具备：

- Go/C++17 异步评测链路、Docker runner 和 gVisor/runsc 支持。
- PostgreSQL 事实源、Redis Stream 投递、judge attempt 和 case result 审计记录。
- 题目版本、测试包上传、结构校验、发布门禁和作者工作台。
- ACM 比赛、邀请码报名、比赛提交策略、live/frozen/final 榜单和快照。
- Prometheus、OpenTelemetry、readiness、dead task recovery 和 Docker smoke gate。

当前主要风险不是缺少普通 CRUD，而是正式比赛遇到错误数据、错误 checker、配置误改或沟通事件时缺少完整处理能力。

## Delivery Principles

1. 每个阶段独立设计、独立计划、独立 PR，避免跨多个领域一次性交付。
2. 先实现后端完整性，再增加组织者 UI；OpenAPI 是跨仓库契约。
3. 所有批量操作必须固定目标集合、可审计、幂等并支持恢复。
4. 已经产生的 judge attempt 和 case result 不删除，只更新当前投影。
5. 比赛开始后默认禁止影响公平性的配置变更；紧急操作必须显式且留痕。
6. 新功能必须进入 Docker smoke 或等价 HTTP 集成测试，不能只依赖内存仓库测试。

## Phase 1: Rejudge Recovery

**Outcome:** 管理员或资源所有者可以针对题目或已结束比赛创建重判批次，系统可靠地重新评测固定的 submission 集合，并展示可审计进度。

### Scope

- 创建、列表、详情、取消重判批次。
- 固定批次明细，防止运行过程中查询集合漂移。
- 只选择终态 submission；不重判仍在排队或运行的 submission。
- 复用现有单 submission judge task，事务性重置为 pending。
- 新 judge attempt 关联 `rejudge_batch_id`。
- 结果消费时幂等更新批次明细与计数。
- 已结束比赛的最终榜单在批次完成后生成新快照。
- 指标、OpenAPI、API 文档和 Docker smoke 覆盖。

### Explicit Deferrals

- 进行中比赛的紧急重判。
- 回滚已完成重判批次。
- 按任意 SQL 风格条件选择 submission。
- 多批次并发重判同一 submission。
- 自动重判所有历史 testcase 版本。

详细执行步骤见 `docs/superpowers/plans/2026-07-12-rejudge-batch-mvp.md`。

## Phase 2: Contest Integrity

**Outcome:** 比赛发布和开赛具有明确门禁，开赛后不能通过普通更新破坏公平性。

### Deliverables

- 比赛状态转换规则：`draft -> published -> running -> ended -> archived`。
- `GET /api/v1/contests/{id}/readiness`，返回稳定 blocker codes。
- 发布门禁检查所有题目已发布、当前校验有效、语言可用、时间窗口合法。
- 开赛后锁定题目集合、开始时间、计分模式和封榜时间。
- 明确的紧急变更命令，要求原因并写入审计事件。
- 比赛开始/结束状态对时间的自动对账。
- 状态、权限、并发更新和 HTTP 契约测试。

### Non-Goals

- 首版不支持多赛制混合。
- 首版不增加复杂审批流。
- 不允许直接编辑数据库绕过状态机。

## Phase 3: Operator Controls

**Outcome:** 比赛组织者能在一个受控界面内发现并处理评测异常。

### Deliverables

- 比赛级评测暂停/恢复，暂停只阻止新任务 dispatch，不杀死正在执行的 sandbox。
- 队列积压、oldest pending age、system error、dead task 和 judge slot 使用情况。
- 题目或语言临时禁用的显式应急操作。
- dead task 批量恢复和失败原因汇总。
- 所有操作包含 actor、reason、request id、时间和对象信息的审计记录。
- SOJ-web 组织者工作台只消费稳定后端契约。

## Phase 4: Contest Communication

**Outcome:** 比赛期间可以统一发布公告和处理题目澄清，不依赖外部群聊作为事实源。

### Deliverables

- 比赛公告：创建、置顶、按时间读取。
- 澄清问题：参赛者提交，组织者私下回复或发布为公开澄清。
- 参赛者只看到自己的私有问题和所有公开澄清。
- 赛中时间线和未读状态。
- 首版使用站内轮询；邮件、WebSocket 和推送后置。

## Phase 5: Participant Administration

**Outcome:** 组织者具备最低限度的参赛资格管理能力。

### Deliverables

- 组织者查看、搜索和导出报名。
- 用户修改报名展示名、主动取消报名。
- 组织者取消或恢复参赛资格，必须填写原因。
- 资格变化立即影响后续提交权限，但不删除历史提交。
- 不引入通用申请审批工作流，除非实际比赛明确需要。

## Phase 6: Judge Compatibility

**Outcome:** 内置 checker 覆盖常见非精确输出题目。

### Deliverables

- token checker。
- absolute/relative epsilon 浮点 checker。
- checker policy 进入题目版本、judge manifest 和发布检查。
- checker 单元测试和真实 runner smoke。
- 自定义 SPJ、validator 和 interactive judging 单独设计，不与本阶段混合。

## Phase 7: Public Platform Basics

仅当 SOJ 开始面向长期公开用户运营时进入：

- 当前用户密码修改。
- 一种安全的账号恢复方式。
- 注册、登录和提交的滥用限制。
- 基本通知偏好。

头像、邮箱登录、图形验证码、题目时空排行榜、公开用户主页和滚榜动画不属于比赛主线，可根据真实需求单独排期。

## Release Gates

每个阶段必须满足：

- `go test ./...`
- `go vet ./...`
- `make compose-config`
- `make compose-config-docker-runner`
- 相关 Docker/HTTP smoke
- OpenAPI 与文档同步
- SOJ-web HTTP adapter 和必要 E2E 通过
- GitHub CI 全部成功

## Recommended Order

1. Phase 1: Rejudge Recovery
2. Phase 2: Contest Integrity
3. Phase 3: Operator Controls
4. Phase 4: Contest Communication
5. Phase 5: Participant Administration
6. Phase 6: Judge Compatibility
7. Phase 7: Public Platform Basics

Phase 4 和 Phase 5 可以在 Phase 2 完成后并行设计，但不能阻塞 Phase 1 至 Phase 3 的比赛可靠性主线。
