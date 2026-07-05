# SOJ

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[English](README.md) | 简体中文

SOJ 是一个使用 Go 编写的开源 Online Judge 后端。当前主线已经切换到 v2 架构：更清晰的 REST API、异步评测流水线、以 PostgreSQL 为事实源的数据模型、Redis Stream 消息投递、兼容 S3 的对象存储，以及独立的 judge-agent 边界，用于承载更安全的代码运行流程。

历史 v1 实现保留在 `archive/v1` 分支。新的后端开发应围绕 v2 的 `cmd/`、`internal/`、`api/`、`docs/` 和 `deploy/` 目录展开。

## 目录

- [功能特性](#功能特性)
- [架构概览](#架构概览)
- [快速开始](#快速开始)
- [开发](#开发)
- [配置](#配置)
- [API 文档](#api-文档)
- [GitHub 工作流](#github-工作流)
- [项目结构](#项目结构)
- [部署说明](#部署说明)
- [路线图](#路线图)
- [许可证](#许可证)
- [联系方式](#联系方式)

## 功能特性

- 用户注册、登录、刷新令牌、当前用户信息和管理员用户管理。
- 题目元数据、题面、标签、测试点压缩包上传、发布校验和题目统计。
- 正式提交、自测运行、评测任务、异步评测请求/结果流、重试、死信和对账循环。
- ACM 赛制比赛、报名、比赛提交策略，以及 live/frozen/final 记分板响应。
- PostgreSQL 迁移、`sqlc`/`pgx` 数据访问，以及面向前端集成的 OpenAPI 契约。
- API、worker 和 judge-agent 进程的 Prometheus 指标。
- 本地 Docker Compose 栈，包含 PostgreSQL、Redis、MinIO、API、worker、judge-agent、migration、seed、Prometheus 和 smoke test。

## 架构概览

SOJ v2 将后端拆分为几个独立命令：

- `soj-api`：Gin HTTP 传输层和 REST API。
- `soj-worker`：评测任务分发、结果消费、重试处理和对账循环。
- `soj-judge-agent`：异步评测代理，消费评测请求并发布评测结果。
- `soj-migrate`：版本化 PostgreSQL 迁移执行器。

Gin 只保留在传输层边界。业务服务接收 `context.Context` 和显式的 `auth.Actor`。PostgreSQL 是 submissions、runs、judge attempts、judge tasks 和 contest results 的事实源。Redis Stream 消息只作为投递提示，可能重复出现；终态写入需要保持幂等。

运行时依赖：

- PostgreSQL：主要关系型数据存储。
- Redis：评测请求/结果流和 consumer group 协调。
- MinIO/S3：源代码、测试点压缩包和未来的大型产物。
- JudgeCore：编译、运行、校验流水线，包含语言配置、checker 逻辑和 sandbox 适配器。
- Prometheus：本地抓取 API、worker 和 judge-agent 指标。

更完整的说明见 [docs/v2-architecture.md](docs/v2-architecture.md)。

## 快速开始

### 环境要求

- Docker with Compose v2
- Go 1.24，本地开发需要
- `curl`、`jq`、`zip` 和 `shasum`，用于 smoke test
- 可选：GitHub CLI `gh`，用于 issue、pull request 和仓库操作

启动一个干净的本地环境：

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

本地栈使用 `fake://accepted` 和 `SOJ_JUDGE_SANDBOX_BACKEND=fake`，因此在具备特权 sandbox 运行时之前，也可以跑通完整异步评测流程。

如需在本地跑真实代码 smoke，可使用仅限开发的 process backend：

```bash
SOJ_ENV=local SOJ_JUDGE_ENDPOINT=agent://local SOJ_JUDGE_SANDBOX_BACKEND=process make up
SMOKE_REAL_JUDGE=1 make smoke
```

process backend 适合本地验证，但不是生产 sandbox。

## 开发

运行主要检查：

```bash
make test
make vet
make compose-config
```

也可以直接执行 Go 和 Docker 命令：

```bash
go test ./...
go vet ./...
docker compose -f deploy/docker-compose.yaml config
```

常用本地命令：

```bash
go run ./cmd/soj-api
go run ./cmd/soj-worker
go run ./cmd/soj-judge-agent
go run ./cmd/soj-migrate --help
```

Docker smoke test 会验证注册、创建题目、上传题面、上传测试点压缩包、发布题目、异步评测、比赛报名、比赛提交、记分板聚合、指标暴露，以及评测结果流持久化。

## 配置

运行时通过 `SOJ_*` 环境变量配置。完整本地示例见 [deploy/env/api.env.example](deploy/env/api.env.example) 和 [deploy/config.example.yaml](deploy/config.example.yaml)。

重要变量：

| 变量 | 作用 |
| --- | --- |
| `SOJ_ENV` | 运行环境名称，默认 `dev`。 |
| `SOJ_HTTP_ADDR` | API 监听地址，默认 `:8080`。 |
| `SOJ_WORKER_HEALTH_ADDR` | Worker 健康检查服务地址，默认 `:8081`。 |
| `SOJ_DATABASE_DSN` | PostgreSQL 连接字符串。真实运行环境必须配置。 |
| `SOJ_REDIS_ADDR` | Redis 地址。 |
| `SOJ_REDIS_STREAM` | 评测请求流，默认 `soj:judge:tasks`。 |
| `SOJ_REDIS_GROUP` | Worker consumer group，默认 `judge-workers`。 |
| `SOJ_STORAGE_ENDPOINT` | 兼容 S3 的对象存储 endpoint。 |
| `SOJ_STORAGE_BUCKET` | 对象存储 bucket。 |
| `SOJ_STORAGE_ACCESS_KEY` | 对象存储 access key。 |
| `SOJ_STORAGE_SECRET_KEY` | 对象存储 secret key。 |
| `SOJ_JWT_SECRET` | JWT 签名密钥。真实部署必须替换。 |
| `SOJ_JUDGE_ENDPOINT` | 评测 endpoint，例如 `fake://accepted` 或 `agent://local`。 |
| `SOJ_JUDGE_TIMEOUT` | 评测超时时间，默认 `30s`。 |

## API 文档

- OpenAPI 契约：[api/openapi.yaml](api/openapi.yaml)
- API 指南：[docs/v2-api-guide.md](docs/v2-api-guide.md)
- Docker 部署：[docs/v2-deploy.md](docs/v2-deploy.md)
- Worker 运维：[docs/v2-worker.md](docs/v2-worker.md)

接口分组：

- Auth：注册、登录、刷新、登出、当前用户。
- Problems：列表、创建、更新、题面、测试点集合、统计。
- Submissions and runs：正式提交、自测运行、结果可见性。
- Contests：比赛 CRUD、报名、live/frozen/final 记分板。
- Admin：用户管理和评测语言管理。

除显式返回空 `204` 或空 `202` 的接口外，所有成功 JSON `2xx` 响应都使用统一响应信封。

## GitHub 工作流

推荐使用 GitHub CLI 处理日常仓库操作：

```bash
gh auth login
gh repo view
gh issue list
gh pr list
```

创建分支和 pull request：

```bash
git checkout -b codex/update-readme
git add README.md README.zh-CN.md
git commit -m "docs: refresh readme"
git push -u origin codex/update-readme
gh pr create --fill
```

如需在 Ubuntu 上安装 `gh`：

```bash
sudo apt-get update
sudo apt-get install -y gh
```

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
internal/observability  日志、健康检查和 Prometheus 指标
```

## 部署说明

当前支持的本地部署方式是 [deploy/docker-compose.yaml](deploy/docker-compose.yaml)。

在对外暴露 SOJ 之前：

- 替换所有本地默认凭据，并使用强 `SOJ_JWT_SECRET`。
- 使用生产 PostgreSQL、Redis 和兼容 S3 的对象存储凭据。
- 将 `/metrics` 保持在私有网络中，或在入口层加保护。
- 运行 `soj-judge-agent` 时不要提供业务数据库凭据。
- `SOJ_JUDGE_SANDBOX_BACKEND=docker` 搭配 Docker runtime `runsc`/gVisor 是生产 sandbox 目标，需要等 Docker runner backend 完成并验证后再用于生产类真实代码执行。
- 不要在开发、测试和本地真实代码 smoke 之外使用 `process` sandbox backend。
- 不要把本地 fake language seed 当作生产语言数据使用。

## 路线图

已知后续工作：

- 在 worker 中自动生成 frozen/final 记分板快照。
- 生产级 judge runner 镜像、Docker backend 和 gVisor/runsc runtime 校验。
- 为 worker 依赖增加更完整的 readiness probe。
- 引入 OpenTelemetry tracing，默认关闭 OTLP export。

## 许可证

SOJ 基于 [MIT License](LICENSE) 开源。

## 联系方式

- WeChat：`sparkyi1026`
- Email：`sparkyi@foxmail.com`
