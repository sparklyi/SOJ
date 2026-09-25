# Sundial Online Judge (SOJ)

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/sparklyi/SOJ/actions/workflows/ci.yml/badge.svg)](https://github.com/sparklyi/SOJ/actions/workflows/ci.yml)

[English](README.md) | 简体中文

**Sundial Online Judge**（简称 SOJ）是一个开源的在线评测系统，覆盖练习、比赛、提交、
记分板和比赛直播。本仓库是 Go 后端，Next.js 前端在
[SOJ-web](https://github.com/sparklyi/SOJ-web)。

| 仓库 | 职责 | 技术栈 |
| --- | --- | --- |
| `SOJ`（本仓库） | REST API、异步评测流水线、比赛与记分板 | Go 1.25、Gin、PostgreSQL、Redis Streams、S3/MinIO |
| [`SOJ-web`](https://github.com/sparklyi/SOJ-web) | 练习、比赛、提交和记分板界面 | Next.js、TypeScript、Tailwind CSS |

## 目录

- [功能特性](#功能特性)
- [架构概览](#架构概览)
- [快速开始](#快速开始)
- [评测运行时](#评测运行时)
- [配置](#配置)
- [API](#api)
- [开发](#开发)
- [部署](#部署)
- [项目结构](#项目结构)
- [路线图](#路线图)
- [许可证](#许可证)
- [相关链接](#相关链接)

## 功能特性

- **账户**：注册、登录、刷新令牌、个人资料、管理员用户管理。
- **题目**：元数据、题面、标签、测试点压缩包上传、发布校验、题目统计。
- **提交与自测**：正式提交、题目页「运行」与练习场、评测任务、异步请求/结果流、重试、死信、对账。
- **比赛**：ACM 赛制、报名、提交策略，以及 live/frozen/final 记分板。
- **平台能力**：PostgreSQL 迁移、`sqlc`/`pgx` 数据访问、OpenAPI 契约、Prometheus 指标、
  可选 OpenTelemetry tracing。
- **本地栈**：Docker Compose 一键拉起 PostgreSQL、Redis、MinIO、API、worker、judge-agent、
  migration、seed、Prometheus，并附带 smoke test。

## 架构概览

SOJ 将后端拆分为四个独立命令：

- `soj-api`：Gin HTTP 传输层和 REST API。
- `soj-worker`：评测任务分发、结果消费、重试处理和对账循环。
- `soj-judge-agent`：异步评测代理，消费评测请求并发布评测结果。
- `soj-migrate`：版本化 PostgreSQL 迁移执行器。

评测链路全程异步：API 把任务写入 PostgreSQL、往 Redis Stream 发一条投递提示后立即返回；
worker 将任务路由给 judge-agent，由后者在沙箱内编译、运行和校验代码；结果沿 Redis Stream
回流，由消费者幂等落库。PostgreSQL 是 submissions、runs、judge attempts、judge tasks 和
contest results 的事实源；Redis Stream 消息只是投递提示，可能重复投递。

Gin 只保留在传输层边界。业务服务接收 `context.Context` 和显式的 `auth.Actor`。

运行时依赖：

- **PostgreSQL**：主要关系型数据存储。
- **Redis**：评测请求/结果流和 consumer group 协调。
- **MinIO/S3**：源代码、测试点压缩包和未来的大型产物。
- **JudgeCore**：编译、运行、校验流水线，包含语言配置、checker 逻辑和 sandbox 适配器。
- **Prometheus**：本地抓取 API、worker 和 judge-agent 指标。
- **OpenTelemetry**：可选 OTLP trace export，默认关闭。

更完整的说明见 [docs/v2-architecture.md](docs/v2-architecture.md)。

## 快速开始

### 环境要求

- Docker with Compose v2
- Go 1.25，本地后端开发需要
- Node.js 22 和 npm，前端开发需要
- `curl`、`jq`、`zip` 和 `shasum`，用于 smoke test
- 可选：GitHub CLI `gh`，用于 issue、pull request 和仓库操作

### 后端：本地栈

启动一个干净的本地环境并跑通端到端 smoke test：

```bash
make down
make up
make smoke
```

等价的原始命令：

```bash
docker compose -f deploy/docker-compose.yaml down -v --remove-orphans
docker compose -f deploy/docker-compose.yaml up --build -d
./deploy/smoke.sh
```

本地服务地址：

| 服务 | 地址 |
| --- | --- |
| API | `http://localhost:8080` |
| Worker health 和 metrics | `http://localhost:8081` |
| Judge-agent health 和 metrics | `http://localhost:8082` |
| MinIO console | `http://localhost:9001` |
| Prometheus | `http://localhost:9090` |

默认栈使用 `judge.sandbox_backend: fake` 和 `fake://accepted`，因此在具备特权 sandbox
运行时之前，也能跑通完整异步评测流程。

### 后端：直接运行进程

```bash
go run ./cmd/soj-migrate
go run ./cmd/soj-api
go run ./cmd/soj-worker
go run ./cmd/soj-judge-agent
```

`soj-migrate --help` 会列出迁移相关参数。

### 前端

前端是独立仓库：[SOJ-web](https://github.com/sparklyi/SOJ-web)。

```bash
git clone git@github.com:sparklyi/SOJ-web.git
cd SOJ-web
npm ci
cp .env.example .env.local   # 默认使用 mock 数据
npm run dev
```

打开 `http://localhost:3000`。主要环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `NEXT_PUBLIC_SOJ_API_MODE` | 本地 `mock`，生产 `http` | 选择 API 适配器。 |
| `NEXT_PUBLIC_SOJ_API_BASE_URL` | 浏览器中为 `/soj-api` | `http` 模式下的公开 API 前缀；不设置则走同源代理。 |
| `SOJ_API_INTERNAL_BASE_URL` | `http://localhost:8080` | 服务端请求和 `/soj-api/*` rewrite 使用的后端地址。 |

浏览器请求统一走同源 `/soj-api/*` 代理，本地不需要额外配置 CORS。

## 评测运行时

### Sandbox backend

| Backend | 运行位置 | 适用场景 |
| --- | --- | --- |
| `fake` | 不执行代码，返回预设结果 | 本地栈、CI 契约检查 |
| `process` | API 或 agent 进程内的子进程 | 仅限本地真实代码验证 |
| `docker` | 由 `soj-judge-agent` 启动的 runner 容器 | 生产目标，搭配 `runsc`/gVisor |

只有 `soj-judge-agent` 允许持有 Docker socket；runner 容器不会拿到 Docker socket 或业务
服务凭据。`process` 不是生产 sandbox。

如需在本地跑真实代码 smoke，可使用仅限开发的 process backend：

```bash
SOJ_ENV=local SOJ_JUDGE_ENDPOINT=agent://local SOJ_JUDGE_SANDBOX_BACKEND=process make up
SMOKE_REAL_JUDGE=1 make smoke
```

### 自测运行

**自测运行**（题目页的「运行」与练习场）走的是和正式提交完全相同的异步链路：API 写入
judge task，worker 发布到 run 流，judge-agent 执行。所以默认的 `agent://local` 就够了，
这也是生产路径——API 进程不执行任何不可信代码。

自测运行有独立的 request stream（`redis.run_stream`），练习场流量不会排在正式提交前面。
默认一个 agent 同时消费两个流并共享沙箱槽；在第二个 agent 上设置
`redis.agent_streams: runs`，就能给练习场独立的容量。

单机部署与本地开发还可以用 `local://`，由 API 进程自己编译执行：

```bash
SOJ_ENV=local SOJ_JUDGE_ENDPOINT=local:// SOJ_JUDGE_SANDBOX_BACKEND=process make up
```

它**拒绝 `docker` 后端**：只有 `soj-judge-agent` 允许持有 Docker socket。worker 和 agent
都在运行时，优先走异步链路。

### 自测运行的保留策略

自测运行是草稿，本来就不需要长期保留——但每次运行都会往对象存储写一个源码对象。因此
worker 会定期清理超过 `retention.run_days` 的已完成自测运行，并**先删对象、后删行**：
这样失败时留下的是行，下一轮会重试；反过来则会留下一个再也找不到的对象。正式提交不受
影响，它们的源码在重测时还要用。

### 真实代码 smoke

通过 Docker runner 容器：

```bash
make smoke-real-docker
```

该目标默认从 GHCR 拉取已发布的 runner images。如需在修改 Dockerfile 时本地构建：

```bash
RUNNER_IMAGES_PREPARE=build make smoke-real-docker
```

Runner images 由 [publish-runner-images.yml](.github/workflows/publish-runner-images.yml) 在
`main` 上 runner image 文件变更、版本 tag 和手动触发时发布。

安装 runsc 后，可以通过 gVisor/runsc 跑同一条链路：

```bash
./scripts/dev/install-gvisor.sh
make smoke-real-gvisor
```

Docker runner 路径使用
[deploy/docker-compose.docker-runner.yaml](deploy/docker-compose.docker-runner.yaml)。
Judge runtime readiness、恢复操作和本地验证证据记录在
[docs/judge-runtime-readiness.md](docs/judge-runtime-readiness.md)；dashboard 查询、alert
解读，以及从 metric pivot 到 trace/attempt 的诊断流程记录在
[docs/observability-trial-loop.md](docs/observability-trial-loop.md)。默认栈不要求 Grafana、
Alertmanager、Jaeger、Tempo 或 OpenTelemetry collector。

## 配置

[deploy/config.yaml](deploy/config.yaml) 就是配置本身：完整 schema 与 Compose 栈
使用的取值都在这里，每个键的默认值定义在 [internal/config](internal/config)，
部署需要改的就是这个文件。

写成占位符（`${NAME}` 或 `${NAME:-default}`）的值在读取文件时从环境解析。这就是
全部的环境变量接口：密钥与按部署变化的旋钮都在文件里声明，加载器除文件路径之外
不再读任何环境变量。

每个命令都支持：

| 参数 | 作用 |
| --- | --- |
| `--config <file>` | 指定配置文件；默认取 `$SOJ_CONFIG_FILE`，再退到存在的 `./config.yaml`。 |
| `--print-config` | 打印脱敏后的生效配置并退出。 |

```bash
go run ./cmd/soj-api --config deploy/config.yaml --print-config
```

随仓库提供的文件引用这些变量（`grep -o '\${[A-Z_]*' deploy/config.yaml` 可直接列出）：

| 占位符 | 作用 |
| --- | --- |
| `SOJ_DATABASE_DSN` | PostgreSQL 连接串；api、worker、migrate 需要。 |
| `SOJ_JWT_SECRET` | JWT 签名密钥；api 需要。 |
| `SOJ_STORAGE_ACCESS_KEY` / `SOJ_STORAGE_SECRET_KEY` | 对象存储凭据。 |
| `SOJ_ENV` | 环境名，默认 `docker`。 |
| `SOJ_JUDGE_ENDPOINT` / `SOJ_JUDGE_SANDBOX_BACKEND` | 评测路由与 sandbox backend。 |
| `SOJ_JUDGE_AGENT_STREAMS` | judge-agent 消费哪些流：`all`（默认）、`submissions` 或 `runs`。 |
| `SOJ_JUDGE_PARALLELISM` / `SOJ_JUDGE_LANGUAGE_SLOTS` / `SOJ_JUDGE_MAX_BATCH` | judge-agent 容量。 |
| `SOJ_JUDGE_RUN_PARALLELISM` / `SOJ_JUDGE_RUN_PER_USER` / `SOJ_JUDGE_RUN_STDIN_MAX_BYTES` | 自测运行限制。 |
| `SOJ_RUN_RETENTION_DAYS` / `SOJ_RUN_RETENTION_INTERVAL` / `SOJ_RUN_RETENTION_BATCH` | 自测运行保留清理。 |
| `SOJ_DOCKER_RUNNER_*` | docker sandbox 的 runtime、workdir 与镜像。 |
| `OTEL_*` | OpenTelemetry service name、resource attributes 与 trace endpoint。 |

把 [deploy/env/api.env.example](deploy/env/api.env.example) 复制成部署环境的
env 即可提供这些值；生产 overlay 与必填变量见
[docs/v2-deploy.md](docs/v2-deploy.md)。

## API

- OpenAPI 契约：[api/openapi.yaml](api/openapi.yaml)
- API 指南：[docs/v2-api-guide.md](docs/v2-api-guide.md)
- Docker 部署：[docs/v2-deploy.md](docs/v2-deploy.md)
- Worker 运维：[docs/v2-worker.md](docs/v2-worker.md)
- Observability trial loop：[docs/observability-trial-loop.md](docs/observability-trial-loop.md)

接口分组：

- Auth：注册、登录、刷新、登出、当前用户。
- Problems：列表、创建、更新、题面、测试点集合、统计。
- Submissions and runs：正式提交、自测运行、结果可见性。
- Contests：比赛 CRUD、报名、live/frozen/final 记分板。
- Admin：用户管理和评测语言管理。

除显式返回空 `204` 或空 `202` 的接口外，所有成功 JSON `2xx` 响应都使用统一响应信封。

## 开发

运行主要检查：

```bash
make test
make vet
make compose-config
make compose-config-docker-runner
```

也可以直接执行底层命令：

```bash
go test ./...
go vet ./...
docker compose -f deploy/docker-compose.yaml config
```

Docker smoke test 会验证注册、创建题目、上传题面、上传测试点压缩包、发布题目、异步评测、
比赛报名、比赛提交、记分板聚合、指标暴露，以及评测结果流持久化。

仓库工作流：从 `main` 拉分支，保持提交聚焦并遵循 Conventional Commits。日常操作推荐使用
GitHub CLI：

```bash
gh auth login
gh issue list
gh pr list
```

## 部署

当前支持的本地部署方式是 [deploy/docker-compose.yaml](deploy/docker-compose.yaml)。

发版由 tag 驱动，服务器不做任何构建：

- 后端：推送 `v*` tag 后，GitHub Actions 构建 `soj-*` 镜像并挂到 GitHub Release；服务器
  轮询到新 tag 后拉取、加载镜像并重启 Compose 栈，健康检查失败会自动回滚。
- 前端：推送 `web-v*` tag 后发布 standalone 构建，服务器通过原子 symlink 切换版本。

在对外暴露 SOJ 之前：

- 替换所有本地默认凭据，并使用强 `SOJ_JWT_SECRET`。
- 使用生产 PostgreSQL、Redis 和兼容 S3 的对象存储凭据。
- 将 `/metrics` 保持在私有网络中，或在入口层加保护。
- 将 tracing backend 和 collector 保持在私有网络中；tracing 默认关闭，必须通过
  `tracing.enabled: true` 显式启用。
- 运行 `soj-judge-agent` 时不要提供业务数据库凭据。
- `judge.sandbox_backend: docker` 搭配 `runsc`/gVisor 是生产 sandbox 目标。
- 生产 judge 节点设置 `env: prod` 和 `agent.runner.runtime: runsc`；如果 runsc 或
  no-op runner probe 不可用，启动会失败。
- 生产环境将 runner 镜像（`agent.runner.go_image`、`agent.runner.cpp17_image`）固定到
  release 或 `sha-*` tag。
- 将 GHCR runner packages 设为 public，或在私有 judge 节点提前登录 `ghcr.io` 后再拉取镜像。
- 不要在开发、测试和本地真实代码 smoke 之外使用 `process` sandbox backend。
- 不要把本地 fake language seed 当作生产语言数据使用。

## 项目结构

```text
api/                    OpenAPI 契约
cmd/soj-api             HTTP API 入口
cmd/soj-judge-agent     Judge agent 入口
cmd/soj-worker          Judge worker 入口
cmd/soj-migrate         PostgreSQL migration 入口
deploy/                 Docker Compose、环境变量示例、Prometheus、smoke test
docs/                   v2 架构、API 指南、worker 和部署文档
internal/app            各命令的运行时组装
internal/auth           Actor、JWT、密码、token 基础能力
internal/user           账户和管理员用户用例
internal/problem        题目、题面、标签、测试点集合
internal/submission     提交、自测、评测任务、worker 逻辑
internal/judge          评测协议和异步事件契约
internal/judgecore      评测核心流水线、语言配置、checker、sandbox 适配器
internal/contest        ACM 比赛、报名、记分板
internal/postgres       SQL 查询和 sqlc 生成代码
internal/queue          Redis Stream 任务队列
internal/storage        兼容 S3 的对象存储
internal/observability  日志、健康检查、Prometheus 指标和 tracing setup
```

## 路线图

- 根据 trial 流量基线调优生产 dashboard 和 alert threshold。
- Dashboard 查询稳定后补充可复用的 Grafana JSON。

## 许可证

Sundial Online Judge 基于 [MIT License](LICENSE) 开源。

## 相关链接

- 后端（本仓库）：<https://github.com/sparklyi/SOJ>
- 前端：<https://github.com/sparklyi/SOJ-web>
- 架构：[docs/v2-architecture.md](docs/v2-architecture.md)
- API 指南：[docs/v2-api-guide.md](docs/v2-api-guide.md)
- Worker 运维：[docs/v2-worker.md](docs/v2-worker.md)
- 部署：[docs/v2-deploy.md](docs/v2-deploy.md)
- Judge runtime readiness：[docs/judge-runtime-readiness.md](docs/judge-runtime-readiness.md)
- 联系方式：微信 `sparkyi1026` · 邮箱 `sparkyi@foxmail.com`
