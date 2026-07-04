# SOJ v2 重构设计

日期：2026-07-04

## 背景

SOJ 当前仓库是较早期手写实现，尚未上线，可以接受破坏性重构。现有代码能通过 `go test ./...` 和 `go vet ./...` 的编译级检查，但缺少测试文件；`docker compose config` 当前失败，原因是 `backend` 依赖未定义的 `redis` 服务。

调研发现的主要问题：

- API、service、repository 广泛传递 `*gin.Context`，HTTP 框架泄漏到业务层。
- 题目、测试点、提交、比赛等核心数据分散在 MySQL、MongoDB、Judge0 PostgreSQL、Redis 中，一致性和迁移成本高。
- 测评提交逻辑中并发等待数量被截断，但仍会启动全部测试点 goroutine，存在漏判和 goroutine 阻塞风险。
- RabbitMQ 消费使用自动 ack，本地 sleep 重试，进程退出会丢失失败消息。
- 中间件依赖数组下标，例如 `mid[1]`、`mid[2]`，权限规则不易审计。
- 默认配置文件包含真实连接串和密钥类配置，Dockerfile、go.mod、Compose 的 Go/服务版本不一致。
- 当前接口形态不统一，例如 `POST /list`，不利于前端联调和 OpenAPI 文档化。

## 目标

- 在当前仓库中构建 v2 架构，保留 Git 历史，旧代码暂作参考，v2 跑通后替换默认入口并清理旧实现。
- 后端按少量服务拆分：API 服务、worker 服务、迁移工具。
- API 使用 Gin，但 Gin 只存在于 HTTP transport 层；业务层统一使用 `context.Context` 和显式 `Actor`。
- 主库统一为 PostgreSQL，数据访问使用 `sqlc + pgx + SQL migration`。
- 判题任务使用 Redis Stream，封装为 `TaskQueue` 接口，后续可替换为其他队列。
- 大文件使用 S3 兼容对象存储，开发环境使用 MinIO，生产可切 COS/S3。
- Judge 后端通过 `JudgeEngine` 抽象，第一版可实现 Judge0 adapter，后续可切 Codenire 或自建 runner。
- REST + OpenAPI 作为 API 契约，方便后续前端联动。
- Docker Compose 作为第一版部署方式。
- 目标规模按中型公开 OJ 设计：10 万级用户，几百并发提交。

## 非目标

- 不保留旧 API 兼容性。
- 不迁移旧 MySQL/MongoDB 数据。
- 不开发前端。
- 不做 Kubernetes 部署。
- 不做多租户/多组织能力。
- 不做邮件、站内信、比赛提醒等通知能力；用户表不保留通知偏好字段。
- 不在第一版实现 IOI 分数制、团队赛、滚榜、气球等高级比赛能力。

## 技术选型

- Go：当前稳定线 Go 1.26.x，工具链、Dockerfile、CI 统一到同一版本线。
- HTTP：Gin。
- 数据库：PostgreSQL。
- 数据访问：`sqlc + pgx`。
- 任务队列：Redis Stream，专用实例或专用逻辑环境，不与缓存/限流混用。
- 对象存储：S3 兼容接口，开发用 MinIO。
- 判题后端：`JudgeEngine` 接口，第一版提供 Judge0 adapter。
- API 文档：OpenAPI 3.x，文件放在 `api/openapi.yaml`。
- 部署：Docker Compose。

## 项目结构

v2 使用 Go 推荐的简洁结构，不采用过度模板化目录。

```text
cmd/
  soj-api/          # HTTP API 服务
  soj-worker/       # 判题和投递补偿后台 worker
  soj-migrate/      # 数据库迁移入口

internal/
  app/              # 服务组装、生命周期、配置加载
  config/           # typed config，支持环境变量覆盖
  httpapi/          # Gin 路由、handler、middleware、response、OpenAPI 对齐
  auth/             # JWT、refresh token、Actor、权限判断
  user/             # 用户与账号模块
  problem/          # 题目、题面版本、测试点元数据
  submission/       # 提交、自测、判题状态机
  contest/          # ACM 比赛、报名、榜单、封榜
  judge/            # JudgeEngine 抽象和 Judge0 adapter
  queue/            # TaskQueue 抽象和 Redis Stream adapter
  storage/          # ObjectStorage 抽象和 S3/MinIO adapter
  postgres/         # pgx pool、sqlc queries、事务辅助
  migrations/       # SQL migration 文件
  observability/    # logger、request id、metrics、health/readiness

api/
  openapi.yaml

deploy/
  docker-compose.yaml
  config.example.yaml

docs/
  v2-architecture.md
  v2-api-guide.md
  v2-deploy.md
  v2-worker.md
```

每个业务模块内部按需要放置 `service.go`、`repository.go`、`handler.go`、`model.go`、`errors.go`、`queries.sql` 等文件。模块边界通过 Go 接口表达，不为空目录服务。

## 服务边界

### soj-api

职责：

- 暴露 REST API。
- 认证、粗粒度登录/角色拦截、请求绑定、响应渲染。
- 调用业务 usecase。
- 创建提交记录和任务 outbox。
- 对自测请求执行短等待，超时返回 run id。

`soj-api` 不直接调用具体 Redis Stream、Judge0 或 S3 SDK，业务模块通过接口依赖这些能力。

### soj-worker

职责：

- 将 PostgreSQL outbox 中未投递任务投递到 Redis Stream。
- 消费 Redis Stream 中的判题任务。
- 调用 `JudgeEngine`。
- 幂等更新提交状态和比赛榜单。
- 处理 pending reclaim、retry、dead-letter。
- 运行 reconciler，修复超时的 dispatching/running judge task，标记 stale run，生成 frozen/final scoreboard snapshot。

### soj-migrate

职责：

- 在空 PostgreSQL 数据库上执行完整 migration。
- 第一版只要求支持 versioned up migration；down migration 不作为门禁。
- Docker Compose 启动时可作为一次性任务运行。

## HTTP 与业务层边界

Gin 只存在于 `internal/httpapi`。进入业务层后统一使用：

```go
DoSomething(ctx context.Context, actor auth.Actor, cmd Command) (Result, error)
```

规则：

- `context.Context` 只负责取消、超时、trace、request id、日志字段传递。
- `auth.Actor` 显式携带用户 ID、角色、认证方式和权限上下文。
- 业务层和 repository 层不 import `github.com/gin-gonic/gin`。
- 资源 owner 权限在 usecase 中判断，路由层只做登录和角色级拦截。
- handler 不直接拼接内部错误消息到响应。

中间件使用显式结构，不使用数组下标：

```go
type MiddlewareSet struct {
    CORS         gin.HandlerFunc
    RateLimit    gin.HandlerFunc
    Auth         gin.HandlerFunc
    RequireAdmin gin.HandlerFunc
    RequireRoot  gin.HandlerFunc
}
```

## 错误处理

业务错误使用统一应用错误：

```go
type AppError struct {
    Code       string
    Message    string
    HTTPStatus int
}
```

错误响应格式：

```json
{
  "data": null,
  "error": {
    "code": "problem.not_found",
    "message": "problem not found"
  },
  "request_id": "..."
}
```

成功响应格式：

```json
{
  "data": {},
  "error": null,
  "request_id": "..."
}
```

`204 no_content` 和明确标注为 `202 empty body` 的接口不使用响应 envelope，响应体为空。其他 `2xx` 响应均使用统一 envelope。

handler 负责把 `AppError` 映射成 HTTP 状态码和响应体。未知错误统一映射为 `internal_error`，日志记录内部细节，响应不暴露堆栈、SQL、DSN、对象存储 key 中的敏感部分。

## 认证与权限

认证方案：

- JWT access token + refresh token。
- access token 短有效期。
- refresh token 只在数据库保存 hash，不保存明文。
- refresh token 支持设备级吊销。
- 配置中的 JWT secret 只能来自环境变量或私有配置，不提交真实值。

权限模型：

- 角色：`user`、`admin`、`root`。
- 资源归属：题目、比赛等资源包含 owner/creator。
- 系统级动作按角色控制。
- 资源级动作按角色 + owner + 参与关系控制。

`auth.Actor` 至少包含：

```text
user_id
role
device_id
request_id
```

## API 契约

第一版使用 REST + OpenAPI，不保留旧接口风格。OpenAPI 文件是前端联调主契约，接口变化先更新 `api/openapi.yaml`。

认证：

```text
POST   /api/v1/auth/register
POST   /api/v1/auth/login
POST   /api/v1/auth/refresh
POST   /api/v1/auth/logout
GET    /api/v1/me
```

题目：

```text
GET    /api/v1/problems
POST   /api/v1/problems
GET    /api/v1/problems/{id}
GET    /api/v1/problems/{id}/statement
PATCH  /api/v1/problems/{id}
DELETE /api/v1/problems/{id}
POST   /api/v1/problems/{id}/statement
POST   /api/v1/problems/{id}/testcase-sets
GET    /api/v1/problems/{id}/stats
```

提交与自测：

```text
POST   /api/v1/submissions
GET    /api/v1/submissions
GET    /api/v1/submissions/{id}
POST   /api/v1/runs
GET    /api/v1/runs/{id}
```

比赛：

```text
GET    /api/v1/contests
POST   /api/v1/contests
GET    /api/v1/contests/{id}
PATCH  /api/v1/contests/{id}
DELETE /api/v1/contests/{id}
POST   /api/v1/contests/{id}/registrations
GET    /api/v1/contests/{id}/scoreboard
```

管理：

```text
GET    /api/v1/admin/users
PATCH  /api/v1/admin/users/{id}
GET    /api/v1/admin/languages
POST   /api/v1/admin/languages/sync
PATCH  /api/v1/admin/languages/{id}
```

分页：

- 普通列表使用 `page`、`page_size`。
- 提交列表保留 cursor 分页扩展空间，第一版仍可提供 `page`、`page_size`。
- 响应包含 `items`、`page`、`page_size`、`total`。

### API 详细契约基线

OpenAPI 必须为每个 endpoint 声明：

- `operationId`。
- 鉴权要求：`public`、`user`、`admin`、`root`。
- 请求 body schema。
- query/path 参数 schema。
- 成功响应 schema。
- 错误响应 schema。
- 可能状态码。

通用状态码：

```text
200 ok
201 created
202 accepted
204 no_content
400 invalid_argument
401 unauthenticated
403 permission_denied
404 not_found
409 conflict
422 validation_failed
429 rate_limited
500 internal_error
```

核心错误码：

```text
auth.invalid_credentials
auth.token_expired
auth.refresh_token_revoked
permission.denied
validation.failed
user.email_exists
problem.not_found
problem.not_publishable
problem.testcase_not_ready
submission.not_found
submission.not_allowed
submission.language_disabled
run.not_found
contest.not_found
contest.not_started
contest.ended
contest.registration_required
contest.scoreboard_hidden
queue.dispatch_failed
judge.engine_unavailable
internal_error
```

主要请求/响应 schema：

```text
RegisterRequest:
  required: email, password, username

LoginRequest:
  required: email, password

AuthResponse:
  required: access_token, refresh_token, expires_in, user

ProblemCreateRequest:
  required: title, slug, difficulty, visibility, time_limit_ms, memory_limit_kb
  optional: tags

ProblemResponse:
  required: id, title, slug, difficulty, visibility, status, tags, limits, owner_user_id, created_at, updated_at
  optional: published_at

ProblemStatementResponse:
  required: problem_id, version, title, description, samples, created_at
  optional: input_description, output_description, hint, source

StatementUpsertRequest:
  required: title, description, samples
  optional: input_description, output_description, hint, source

TestcaseSetCreateRequest:
  required: multipart archive file, checksum_sha256, case_count

TestcaseSetResponse:
  required: id, problem_id, version, checksum_sha256, size_bytes, case_count, status, is_current, created_at

SubmissionCreateRequest:
  required: problem_id, language_id, source_code
  optional: contest_id

SubmissionResponse:
  required: id, user_id, problem_id, language_id, status, score, submitted_at, updated_at
  optional: contest_id, time_ms, memory_kb, judged_at, error_message

RunCreateRequest:
  required: problem_id, language_id, source_code
  optional: stdin

RunResponse:
  required: id, user_id, problem_id, language_id, status, created_at, updated_at
  optional: stdout, stderr, compile_output, time_ms, memory_kb, finished_at, error_message

ContestCreateRequest:
  required: title, visibility, start_at, end_at, freeze_at, problems
  optional: description

ScoreboardResponse:
  required: contest_id, view, generated_at, rows

ScoreboardRow:
  required: rank, user_id, display_name, accepted_count, penalty_minutes, cells

ScoreboardCell:
  required: problem_id, alias, status, attempts, penalty_minutes
  optional: accepted_at, last_submission_id, frozen_attempts

ProblemStatsResponse:
  required: problem_id, total_submissions, accepted_submissions, status_counts

ContestResponse:
  required: id, owner_user_id, title, visibility, status, start_at, end_at, freeze_at, problems, created_at, updated_at
  optional: description

ContestRegistrationResponse:
  required: id, contest_id, user_id, display_name, email, status, registered_at

UserResponse:
  required: id, email, username, role, status, created_at, updated_at
  optional: avatar_url, bio

LanguageResponse:
  required: id, engine, engine_language_id, name, default_time_limit_ms, default_memory_limit_kb, enabled
  optional: version, compile_command, run_command

Page<T>:
  required: items, page, page_size, total
```

端点契约矩阵：

```text
POST /api/v1/auth/register
  auth: public
  request: RegisterRequest
  success: 201 AuthResponse
  errors: 400, 409, 422, 500

POST /api/v1/auth/login
  auth: public
  request: LoginRequest
  success: 200 AuthResponse
  errors: 400, 401, 422, 500

POST /api/v1/auth/refresh
  auth: public refresh token
  request: refresh_token
  success: 200 AuthResponse
  errors: 400, 401, 500

POST /api/v1/auth/logout
  auth: user
  success: 204
  errors: 401, 500

GET /api/v1/me
  auth: user
  success: 200 UserResponse
  errors: 401, 500

GET /api/v1/problems
  auth: public
  query: page, page_size, difficulty, tag, keyword, status optional for admin/root
  success: 200 Page<ProblemResponse>
  errors: 400, 500

POST /api/v1/problems
  auth: user
  request: ProblemCreateRequest
  success: 201 ProblemResponse
  errors: 400, 401, 403, 409, 422, 500

GET /api/v1/problems/{id}
  auth: public, private/contest_only requires owner/admin/root or contest access
  success: 200 ProblemResponse
  errors: 401, 403, 404, 500

GET /api/v1/problems/{id}/statement
  auth: same as problem detail
  success: 200 ProblemStatementResponse
  errors: 401, 403, 404, 500

PATCH /api/v1/problems/{id}
  auth: owner/admin/root
  request: partial ProblemCreateRequest plus status
  success: 200 ProblemResponse
  errors: 400, 401, 403, 404, 409, 422, 500

DELETE /api/v1/problems/{id}
  auth: owner/admin/root
  success: 204
  errors: 401, 403, 404, 409, 500

POST /api/v1/problems/{id}/statement
  auth: owner/admin/root
  request: StatementUpsertRequest
  success: 201 ProblemStatementResponse
  errors: 400, 401, 403, 404, 422, 500

POST /api/v1/problems/{id}/testcase-sets
  auth: owner/admin/root
  request: multipart TestcaseSetCreateRequest
  success: 201 TestcaseSetResponse
  errors: 400, 401, 403, 404, 409, 422, 500

GET /api/v1/problems/{id}/stats
  auth: public for public problem, otherwise same as problem detail
  success: 200 ProblemStatsResponse
  errors: 401, 403, 404, 500

POST /api/v1/submissions
  auth: user
  request: SubmissionCreateRequest
  success: 202 SubmissionResponse
  errors: 400, 401, 403, 404, 409, 422, 500

GET /api/v1/submissions
  auth: user
  query: page, page_size, problem_id, contest_id, status, user_id admin/root only
  success: 200 Page<SubmissionResponse>
  errors: 400, 401, 403, 500

GET /api/v1/submissions/{id}
  auth: owner/admin/root plus contest visibility rules
  success: 200 SubmissionResponse
  errors: 401, 403, 404, 500

POST /api/v1/runs
  auth: user
  request: RunCreateRequest
  success: 200 RunResponse when finished within short wait, 202 RunResponse when still running
  errors: 400, 401, 403, 404, 422, 500

GET /api/v1/runs/{id}
  auth: owner/admin/root
  success: 200 RunResponse
  errors: 401, 403, 404, 500

GET /api/v1/contests
  auth: public
  query: page, page_size, status, visibility, keyword
  success: 200 Page<ContestResponse>
  errors: 400, 500

POST /api/v1/contests
  auth: user
  request: ContestCreateRequest
  success: 201 ContestResponse
  errors: 400, 401, 403, 409, 422, 500

GET /api/v1/contests/{id}
  auth: public for public contest, private requires registration/owner/admin/root
  success: 200 ContestResponse
  errors: 401, 403, 404, 500

PATCH /api/v1/contests/{id}
  auth: owner/admin/root
  request: partial ContestCreateRequest plus status
  success: 200 ContestResponse
  errors: 400, 401, 403, 404, 409, 422, 500

DELETE /api/v1/contests/{id}
  auth: owner/admin/root
  success: 204
  errors: 401, 403, 404, 409, 500

POST /api/v1/contests/{id}/registrations
  auth: user
  request: display_name, email, invite_code optional
  success: 201 ContestRegistrationResponse
  errors: 400, 401, 403, 404, 409, 422, 500

GET /api/v1/contests/{id}/scoreboard
  auth: public for public contest, private requires registration/owner/admin/root
  query: view optional live|frozen|final
  success: 200 ScoreboardResponse
  errors: 400, 401, 403, 404, 500

GET /api/v1/admin/users
  auth: root
  query: page, page_size, role, status, keyword
  success: 200 Page<UserResponse>
  errors: 400, 401, 403, 500

PATCH /api/v1/admin/users/{id}
  auth: root
  request: role, status, username, bio optional
  success: 200 UserResponse
  errors: 400, 401, 403, 404, 422, 500

GET /api/v1/admin/languages
  auth: admin/root
  success: 200 Page<LanguageResponse>
  errors: 401, 403, 500

POST /api/v1/admin/languages/sync
  auth: root
  success: 202 empty body
  errors: 401, 403, 500

PATCH /api/v1/admin/languages/{id}
  auth: admin/root
  request: enabled, limits optional
  success: 200 LanguageResponse
  errors: 400, 401, 403, 404, 422, 500
```

接口鉴权基线：

```text
POST /api/v1/auth/register                    public
POST /api/v1/auth/login                       public
POST /api/v1/auth/refresh                     public, valid refresh token required
POST /api/v1/auth/logout                      user
GET  /api/v1/me                               user

GET  /api/v1/problems                         public, private results only for owner/admin/root
POST /api/v1/problems                         user
GET  /api/v1/problems/{id}/statement          same visibility as problem detail; returns current statement
PATCH/DELETE /api/v1/problems/{id}            owner/admin/root
POST /api/v1/problems/{id}/statement          owner/admin/root; creates new version and marks it current
POST /api/v1/problems/{id}/testcase-sets      owner/admin/root

POST /api/v1/submissions                      user
GET  /api/v1/submissions                      user, admin/root can filter broader
GET  /api/v1/submissions/{id}                 owner/admin/root, contest visibility rules apply
POST /api/v1/runs                             user
GET  /api/v1/runs/{id}                        owner/admin/root

POST/PATCH/DELETE /api/v1/contests            user for own contests, admin/root for all
POST /api/v1/contests/{id}/registrations      user
GET  /api/v1/contests/{id}/scoreboard         public for public contests after publish, view rules apply
```

`ContestCreateRequest.problems` 是有序数组，元素为 `{problem_id, alias}`。`sort_order` 使用数组顺序从 1 开始生成；`alias` 必须在比赛内唯一，建议使用 `A`、`B`、`C`。

## PostgreSQL schema 设计

通用规则：

- 所有时间字段使用 `timestamptz`。
- 主键使用 `bigint generated by default as identity`。
- 状态字段使用 `text + check constraint`，便于 migration。
- 外部接口第一版可暴露数字 ID。
- 大文本和大文件存对象存储，数据库保存元数据。
- 需要审计的表保留 `created_at`、`updated_at`，删除优先软删除或状态归档。

### users

字段：

```text
id bigint primary key
email text not null
password_hash text not null
username text not null
avatar_url text
bio text
role text not null check (role in ('user', 'admin', 'root'))
status text not null check (status in ('active', 'disabled', 'deleted'))
created_at timestamptz not null
updated_at timestamptz not null
```

索引：

```text
unique index users_email_lower_uidx on users (lower(email))
index users_status_idx on users (status)
index users_role_idx on users (role)
```

### refresh_tokens

字段：

```text
id bigint primary key
user_id bigint not null references users(id)
token_hash text not null
device_id text not null
user_agent text
ip inet
expires_at timestamptz not null
revoked_at timestamptz
created_at timestamptz not null
```

索引：

```text
unique index refresh_tokens_token_hash_uidx on refresh_tokens (token_hash)
index refresh_tokens_user_id_idx on refresh_tokens (user_id)
index refresh_tokens_expires_at_idx on refresh_tokens (expires_at)
index refresh_tokens_active_idx on refresh_tokens (user_id, device_id) where revoked_at is null
```

### problems

字段：

```text
id bigint primary key
owner_user_id bigint not null references users(id)
title text not null
slug text not null
difficulty text not null check (difficulty in ('easy', 'medium', 'hard'))
visibility text not null check (visibility in ('private', 'public', 'contest_only'))
status text not null check (status in ('draft', 'published', 'archived'))
time_limit_ms integer not null
memory_limit_kb integer not null
created_at timestamptz not null
updated_at timestamptz not null
published_at timestamptz
```

索引：

```text
unique index problems_slug_uidx on problems (slug)
index problems_status_visibility_idx on problems (status, visibility)
index problems_owner_user_id_idx on problems (owner_user_id)
index problems_difficulty_idx on problems (difficulty)
```

### tags

字段：

```text
id bigint primary key
name text not null
slug text not null
```

索引：

```text
unique index tags_slug_uidx on tags (slug)
```

### problem_tags

字段：

```text
problem_id bigint not null references problems(id) on delete cascade
tag_id bigint not null references tags(id) on delete cascade
primary key (problem_id, tag_id)
```

索引：

```text
index problem_tags_tag_id_idx on problem_tags (tag_id)
```

### problem_statements

字段：

```text
id bigint primary key
problem_id bigint not null references problems(id) on delete cascade
version integer not null
title text not null
description text not null
input_description text
output_description text
samples jsonb not null default '[]'::jsonb
hint text
source text
is_current boolean not null default false
created_at timestamptz not null
```

索引：

```text
unique index problem_statements_problem_version_uidx on problem_statements (problem_id, version)
unique index problem_statements_current_uidx on problem_statements (problem_id) where is_current
index problem_statements_problem_id_idx on problem_statements (problem_id)
```

`GET /api/v1/problems/{id}/statement` 始终读取 `is_current = true` 的题面版本。创建新题面版本时，必须在同一事务中把旧版本 `is_current` 置为 false，并把新版本置为 true。

### testcase_sets

字段：

```text
id bigint primary key
problem_id bigint not null references problems(id) on delete cascade
version integer not null
storage_key text not null
checksum_sha256 text not null
size_bytes bigint not null
case_count integer not null
status text not null check (status in ('uploading', 'ready', 'disabled'))
is_current boolean not null default false
created_by bigint not null references users(id)
created_at timestamptz not null
```

索引：

```text
unique index testcase_sets_problem_version_uidx on testcase_sets (problem_id, version)
index testcase_sets_problem_ready_idx on testcase_sets (problem_id) where status = 'ready'
unique index testcase_sets_current_uidx on testcase_sets (problem_id) where is_current
```

题目发布规则：

- `problems.status = 'published'` 前必须存在 `problem_statements.is_current = true`。
- `problems.status = 'published'` 前必须存在 `testcase_sets.is_current = true and status = 'ready'`。
- 正式提交使用提交创建时的 current ready testcase set，并把版本快照写入 submission。
- 切换 current testcase set 不影响已创建但未完成的 submission；它们继续使用创建时记录的 testcase set。

测试点上传生命周期：

- `POST /api/v1/problems/{id}/testcase-sets` 使用 multipart 上传压缩包，API 在请求内同步完成基础校验。
- 基础校验包括：压缩包可解开、文件数量和 `case_count` 一致、输入/输出文件命名合法、总大小不超过配置上限、sha256 与请求一致。
- 校验失败时返回 `422 problem.testcase_not_ready`，不创建 `testcase_sets` 记录，不写 current。
- 校验成功后先把压缩包写入对象存储，再在 PostgreSQL 事务中创建新版本 `testcase_sets.status = 'ready'`。
- 创建新版本前必须在事务中对对应 `problems` 行执行 `select ... for update`，同一题的测试点上传和 current 切换按 problem 行锁串行化。
- 新版本号在持有 problem 行锁后计算为 `max(version) + 1`。
- 同一事务中将该 problem 的旧 current testcase set 置为 `is_current = false`，新版本置为 `is_current = true`。
- 第一版不提供单独的 current 切换接口；切换 current 只能通过上传并校验一个新的 ready testcase set 完成。
- 禁用旧 testcase set 只影响未来查询，不影响已记录 `testcase_set_id` 的 submission。
- 对象存储写入成功但 PostgreSQL 事务失败时，API 尽力删除刚写入的对象；删除失败则记录 orphan artifact 日志，后续由对象存储清理任务按未被 `artifacts` 引用的 key 清理。

### languages

字段：

```text
id bigint primary key
engine text not null
engine_language_id text not null
name text not null
version text
compile_command text
run_command text
default_time_limit_ms integer not null
default_memory_limit_kb integer not null
enabled boolean not null default true
created_at timestamptz not null
updated_at timestamptz not null
```

索引：

```text
unique index languages_engine_language_uidx on languages (engine, engine_language_id)
index languages_enabled_idx on languages (enabled)
```

### runs

`runs` 是自测记录，不参与正式提交列表、比赛榜单和罚时。

字段：

```text
id bigint primary key
user_id bigint not null references users(id)
problem_id bigint not null references problems(id)
language_id bigint not null references languages(id)
status text not null check (
  status in (
    'queued',
    'running',
    'accepted',
    'wrong_answer',
    'compile_error',
    'runtime_error',
    'time_limit',
    'memory_limit',
    'system_error',
    'canceled'
  )
)
source_artifact_id bigint references artifacts(id)
stdin text
stdout text
stderr text
compile_output text
time_ms integer
memory_kb integer
error_message text
created_at timestamptz not null
finished_at timestamptz
updated_at timestamptz not null
```

索引：

```text
index runs_user_created_idx on runs (user_id, created_at desc)
index runs_status_updated_idx on runs (status, updated_at)
index runs_problem_id_idx on runs (problem_id)
```

`runs` 是用户自定义 stdin 的单次运行记录，不跑官方测试点，不产生题目通过语义。`accepted` 在 run 中表示进程正常结束；`wrong_answer` 不用于 run。`runs` 的 stdout、stderr、compile output 第一版限制大小并直接存表，单字段默认最多保留 64 KiB。超过上限时截断到 64 KiB，并在 `error_message` 或响应扩展字段中标记 `output_truncated = true`；第一版不为自测超大输出创建 artifact，避免自测输出拖垮热表和对象存储。
`runs.source_artifact_id` 引用后文的 `artifacts(id)`，migration 可先创建列，再在 `artifacts` 创建后添加外键。

### artifacts

字段：

```text
id bigint primary key
owner_type text not null check (owner_type in ('submission', 'run', 'problem', 'testcase'))
owner_id bigint not null
kind text not null check (kind in ('source', 'stdout', 'stderr', 'compile_output', 'judge_log', 'attachment', 'testcase_archive'))
storage_key text not null
checksum_sha256 text not null
size_bytes bigint not null
content_type text not null
created_at timestamptz not null
```

索引：

```text
index artifacts_owner_idx on artifacts (owner_type, owner_id)
index artifacts_kind_idx on artifacts (kind)
unique index artifacts_storage_key_uidx on artifacts (storage_key)
```

`artifacts.owner_id` 是多态引用，不设置数据库外键；写入 artifact 必须由对应业务 usecase 在同一业务流程中校验 owner 是否存在。`submissions.source_artifact_id` 和 `runs.source_artifact_id` 直接引用 `artifacts(id)`，用于源码定位。

### submissions

字段：

```text
id bigint primary key
user_id bigint not null references users(id)
problem_id bigint not null references problems(id)
contest_id bigint references contests(id)
language_id bigint not null references languages(id)
testcase_set_id bigint not null references testcase_sets(id)
status text not null check (
  status in (
    'queued',
    'running',
    'accepted',
    'wrong_answer',
    'compile_error',
    'runtime_error',
    'time_limit',
    'memory_limit',
    'system_error',
    'canceled'
  )
)
source_artifact_id bigint references artifacts(id)
time_ms integer
memory_kb integer
score integer not null default 0
error_message text
submitted_at timestamptz not null
judged_at timestamptz
updated_at timestamptz not null
```

索引：

```text
index submissions_user_submitted_idx on submissions (user_id, submitted_at desc)
index submissions_problem_status_idx on submissions (problem_id, status)
index submissions_contest_submitted_idx on submissions (contest_id, submitted_at desc)
index submissions_status_updated_idx on submissions (status, updated_at)
index submissions_language_id_idx on submissions (language_id)
```

`submissions.contest_id` 引用后文的 `contests(id)`，migration 可先创建列，再在 `contests` 创建后添加外键。

### judge_tasks

该表是 Redis Stream 投递审计和补偿 outbox，不是主队列。

字段：

```text
id bigint primary key
submission_id bigint not null references submissions(id)
stream_id text
status text not null check (status in ('pending', 'dispatching', 'dispatched', 'running', 'done', 'dead'))
attempts integer not null default 0
next_run_at timestamptz not null
last_error text
created_at timestamptz not null
updated_at timestamptz not null
```

索引：

```text
unique index judge_tasks_submission_id_uidx on judge_tasks (submission_id)
index judge_tasks_status_next_run_idx on judge_tasks (status, next_run_at)
index judge_tasks_stream_id_idx on judge_tasks (stream_id)
```

### contests

字段：

```text
id bigint primary key
owner_user_id bigint not null references users(id)
title text not null
description text
visibility text not null check (visibility in ('public', 'private'))
status text not null check (status in ('draft', 'published', 'running', 'ended', 'archived'))
start_at timestamptz not null
end_at timestamptz not null
freeze_at timestamptz not null
invite_code_hash text
created_at timestamptz not null
updated_at timestamptz not null
```

索引：

```text
index contests_status_start_idx on contests (status, start_at)
index contests_owner_user_id_idx on contests (owner_user_id)
index contests_visibility_idx on contests (visibility)
```

### contest_problems

字段：

```text
contest_id bigint not null references contests(id) on delete cascade
problem_id bigint not null references problems(id)
alias text not null
sort_order integer not null
primary key (contest_id, problem_id)
```

索引：

```text
unique index contest_problems_alias_uidx on contest_problems (contest_id, alias)
unique index contest_problems_sort_uidx on contest_problems (contest_id, sort_order)
```

### contest_registrations

字段：

```text
id bigint primary key
contest_id bigint not null references contests(id) on delete cascade
user_id bigint not null references users(id)
display_name text not null
email text not null
status text not null check (status in ('active', 'canceled'))
registered_at timestamptz not null
```

索引：

```text
unique index contest_registrations_contest_user_uidx on contest_registrations (contest_id, user_id)
index contest_registrations_user_id_idx on contest_registrations (user_id)
index contest_registrations_contest_status_idx on contest_registrations (contest_id, status)
```

### contest_problem_results

字段：

```text
contest_id bigint not null references contests(id) on delete cascade
user_id bigint not null references users(id)
problem_id bigint not null references problems(id)
status text not null check (status in ('none', 'attempted', 'accepted', 'frozen'))
attempts integer not null default 0
accepted_at timestamptz
penalty_minutes integer not null default 0
last_submission_id bigint references submissions(id)
updated_at timestamptz not null
primary key (contest_id, user_id, problem_id)
```

索引：

```text
index contest_problem_results_contest_problem_idx on contest_problem_results (contest_id, problem_id)
index contest_problem_results_last_submission_idx on contest_problem_results (last_submission_id)
```

### contest_score_snapshots

字段：

```text
id bigint primary key
contest_id bigint not null references contests(id) on delete cascade
kind text not null check (kind in ('live', 'frozen', 'final'))
payload jsonb not null
generated_at timestamptz not null
```

索引：

```text
index contest_score_snapshots_contest_kind_generated_idx on contest_score_snapshots (contest_id, kind, generated_at desc)
```

实时榜可从 `contest_problem_results` 聚合；高并发比赛可读取 snapshot 缓存冻结榜和最终榜。

## Redis Stream 设计

Stream：

```text
soj:judge:tasks
soj:judge:tasks:dead
```

Consumer group：

```text
judge-workers
```

worker 启动时使用 `XGROUP CREATE soj:judge:tasks judge-workers $ MKSTREAM` 确保 group 存在；如果已存在则忽略 BUSYGROUP 错误。默认消费批量 `limit = 16`，默认阻塞时间 `block = 5s`，默认 stale claim 阈值 `minIdle = 2 * JudgeEngine request timeout`。

消息 payload 只包含小字段：

```json
{
  "task_id": "123",
  "submission_id": "456",
  "priority": "normal",
  "attempt": "1"
}
```

### TaskQueue 接口

业务 worker 只依赖 `TaskQueue`，不直接调用 Redis SDK：

```go
type TaskQueue interface {
    Publish(ctx context.Context, task QueueTask) (streamID string, err error)
    Consume(ctx context.Context, group, consumer string, limit int, block time.Duration) ([]QueueMessage, error)
    Ack(ctx context.Context, group string, msg QueueMessage) error
    ClaimStale(ctx context.Context, group, consumer string, minIdle time.Duration, limit int) ([]QueueMessage, error)
    DeadLetter(ctx context.Context, msg QueueMessage, reason string) error
}
```

职责边界：

- queue adapter 负责 Redis Stream 命令、ack、claim、dead-letter 写入。
- worker usecase 负责读取 PostgreSQL task 状态、判断是否可执行、调用 JudgeEngine、决定 retry/dead。
- dispatcher 负责把 `judge_tasks` outbox 投递到 queue。
- PostgreSQL 是事实源，Redis 只负责分发和消费协调。

规则：

- API 在 PostgreSQL 事务中创建 `submissions` 和 `judge_tasks`。
- dispatcher 使用 `select ... for update skip locked` 从 `judge_tasks where status = 'pending' and next_run_at <= now()` 抢占任务。
- dispatcher 先把 `judge_tasks.status` 更新为 `dispatching` 并提交短事务，再执行 `XADD`。
- `XADD` 成功后将 `judge_tasks.status` 更新为 `dispatched` 并记录 `stream_id`。
- 如果 dispatcher 在 `dispatching` 后、`XADD` 前崩溃，reconciler 将超时的 `dispatching` 任务改回 `pending`。
- 如果 dispatcher 在 `XADD` 后、记录 `stream_id` 前崩溃，reconciler 允许重新投递；worker 通过 `task_id` 和 PostgreSQL 状态保证重复消息幂等。
- worker 使用 consumer group 消费，业务落库成功后才 `XACK`。
- worker 开始执行前把 `judge_tasks.status` 从 `dispatched` 更新为 `running`，同时把 `submissions.status` 从 `queued` 更新为 `running`；如果任一状态已进入终态，worker 直接走幂等 ack。
- worker 周期性使用 `XPENDING` 和 `XAUTOCLAIM` 回收超时未 ack 的消息。
- `XAUTOCLAIM` 拿回消息后，如果 PostgreSQL 中 `judge_tasks.status = 'running'` 且 submission 未终态，worker 将其视为上一个 worker 崩溃后的未完成任务。新 worker 允许重新执行判题；结果写入仍受 submission 终态幂等保护。
- reconciler 周期性扫描 `judge_tasks.status = 'running' and updated_at < now() - running_timeout`，并将对应任务改回 `pending`、submission 改回 `queued`。`running_timeout` 默认 10 分钟，可配置，必须大于 JudgeEngine 单次请求超时。
- 可重试错误仅限 JudgeEngine 不可用、网络超时、worker 临时故障等基础设施错误；同一事务中更新 `judge_tasks.status = 'pending'`、`attempts = attempts + 1`、`next_run_at = now() + backoff`，并把 `submissions.status` 改回 `queued`；事务提交后 worker `XACK` 原消息。
- 重试不使用单独 retry stream；延迟由 PostgreSQL `next_run_at` 和 dispatcher 控制，避免 Redis Stream 自建延迟队列语义。
- 最大重试次数默认 5 次；backoff 使用 5s、30s、2m、10m、30m。
- 超过最大次数进入 `soj:judge:tasks:dead`，`judge_tasks.status = 'dead'`，`submissions.status = 'system_error'`。
- dead-letter 顺序：先在 PostgreSQL 事务中更新 `judge_tasks.status = 'dead'` 和 `submissions.status = 'system_error'`，事务提交后写入 `soj:judge:tasks:dead`，最后 `XACK` 原消息。dead stream 写入失败时仍保留 PostgreSQL dead 状态并记录错误日志，仍然 `XACK` 原消息，运维以 PostgreSQL dead 状态为事实源。
- Redis Stream 不是事实源，事实源是 PostgreSQL 中的 submission 和 judge task 状态。

`judge_tasks.status` 取值扩展为：

```text
pending
dispatching
dispatched
running
done
dead
```

对应 schema check constraint 使用同一集合。

## JudgeEngine 抽象

业务层依赖接口：

```go
type JudgeEngine interface {
    Run(ctx context.Context, req RunRequest) (RunResult, error)
    SyncLanguages(ctx context.Context) ([]Language, error)
}
```

`RunRequest` 包含：

```text
language_id
source artifact/object key
testcase set object key for official submission
time limit
memory limit
stdin for self-run when testcase set object key is empty
```

`RunResult` 包含：

```text
status
time_ms
memory_kb
stdout artifact
stderr artifact
compile_output artifact
case results summary
engine metadata
```

第一版 Judge0 adapter 负责把 SOJ 的请求/结果映射到 Judge0 API。业务层不拼 Judge0 URL、不依赖 Judge0 token、不读取 Judge0 数据库。

## 对象存储

对象存储接口：

```go
type ObjectStorage interface {
    Put(ctx context.Context, key string, body io.Reader, meta ObjectMeta) error
    Get(ctx context.Context, key string) (io.ReadCloser, ObjectMeta, error)
    Delete(ctx context.Context, key string) error
}
```

存储内容：

- 测试点压缩包。
- 用户提交源码。
- stdout、stderr、compile output、judge log 等大文本。
- 题目附件。

对象 key 由服务端生成，包含业务前缀和不可预测 ID，不使用用户上传文件名作为最终 key。

## 提交流程

正式提交：

1. API 解析 JWT，构造 `Actor`。
2. handler 绑定请求，调用 `submission.CreateSubmission(ctx, actor, cmd)`。
3. usecase 校验题目状态、语言可用性、比赛可提交性和用户权限。
4. 源码写入对象存储，生成 `artifacts`。
5. PostgreSQL 事务创建 `submissions`，状态为 `queued`。
6. 同一事务创建 `judge_tasks`，状态为 `pending`。
7. dispatcher 投递 Redis Stream。
8. worker 消费任务，更新 submission 为 `running`。
9. worker 调用 `JudgeEngine`。
10. worker 在事务中写结果、artifact、比赛结果和 task 状态。
11. worker `XACK`。

自测：

- API 创建 `runs` 记录，状态为 `queued`。自测只运行用户提供的 `stdin`，不使用官方 testcase set，不产生题目 AC/WA 判定。
- API 启动一个受服务生命周期管理的短任务执行 `JudgeEngine`，不进入正式提交 Redis Stream，避免自测挤占正式提交队列。
- 短任务开始前将 `runs.status` 更新为 `running`。
- API 等待 3 到 5 秒：如果短任务完成，直接返回终态 `RunResponse`；如果未完成，返回 `202 accepted` 和 `run_id`。
- 短任务在请求返回后继续执行，但必须绑定服务级 context 和超时，不依赖已经结束的 Gin request context。
- 短任务完成后更新 `runs` 为终态并保存受限大小的 stdout、stderr、compile output。
- 前端通过 `GET /api/v1/runs/{id}` 轮询未完成的自测。
- 自测不写正式榜单，不影响比赛罚时。
- 自测是交互辅助能力，不保证进程崩溃后的自动重试。API 进程退出时仍处于 `queued` 或 `running` 且 `updated_at < now() - run_timeout` 的 run，由 `soj-worker` 内的 reconciler 标记为 `system_error`，`error_message = 'run interrupted'`。
- reconciler 默认每 30 秒扫描一次 stale run，只扫描 `status in ('queued', 'running')` 且超过 `run_timeout` 的记录。
- `run_timeout` 默认 60 秒，可配置，必须大于自测 JudgeEngine 请求超时。

## 提交状态机

允许状态：

```text
queued
running
accepted
wrong_answer
compile_error
runtime_error
time_limit
memory_limit
system_error
canceled
```

转换规则：

- `queued -> running`
- `queued -> canceled`
- `running -> accepted`
- `running -> wrong_answer`
- `running -> compile_error`
- `running -> runtime_error`
- `running -> time_limit`
- `running -> memory_limit`
- `running -> system_error`
- `running -> canceled`

终态不可被非补偿流程覆盖。重复消费同一个任务时，如果 submission 已是终态，worker 只将任务标记为 done 并 ack，不重复更新榜单。

`runs` 使用同一组状态和转换规则，但不创建 `judge_tasks`，不进入 Redis Stream，不更新比赛榜单。`GET /api/v1/runs/{id}` 返回 `RunResponse`，只有 run owner、admin、root 可访问。

## 比赛设计

第一版只支持基础 ACM：

- 比赛创建、发布、归档。
- 公开/私有比赛。
- 报名。
- 题集和题目别名。
- 赛时提交。
- 封榜。
- 榜单查询。

榜单规则：

- accepted 数量优先。
- accepted 数量相同按罚时升序。
- 单题罚时 = 该题首次 AC 距比赛开始的分钟数 + 首次 AC 前错误提交次数 * 20 分钟。
- 未最终 AC 的题目不计入总罚时；其错误提交只显示尝试次数，不影响排序罚时。
- 首次 AC 后该题后续提交不再影响 accepted 数量、罚时和尝试次数。
- 封榜后普通用户看到 frozen 视图。
- admin/root 和比赛 owner 可看实时视图。
- 比赛结束后可生成 final snapshot。

`contest_problem_results` 是参赛者每题状态事实表。`contest_score_snapshots` 用于缓存冻结榜和最终榜。

榜单视图和权限：

```text
live:
  admin/root/contest owner 可查看；普通用户仅在 freeze_at 前可查看等价 live 结果。

frozen:
  freeze_at <= now < end_at 时普通用户默认查看 frozen。frozen snapshot 在 `freeze_at` 后由 worker/reconciler 生成，内容只包含 `freeze_at` 前已判定的提交结果。snapshot 尚未生成时，API 使用 `submitted_at < freeze_at and judged_at <= freeze_at` 的提交即时聚合 frozen 视图并异步补写 snapshot。

final:
  now >= end_at 后所有可访问比赛的人默认查看 final。final snapshot 由 worker/reconciler 在比赛结束后生成；如果 snapshot 尚未生成，API 从 `contest_problem_results` 即时聚合 final 视图并异步补写 snapshot。
```

`GET /api/v1/contests/{id}/scoreboard` 支持可选 query `view=live|frozen|final`。未传时按当前时间和权限选择默认视图；无权访问所请求视图时返回 `403 contest.scoreboard_hidden`。

Scoreboard row/cell 语义：

- `rank` 使用竞赛排名；accepted 和 penalty 完全相同的用户共享同一 rank，下一名跳过占用名次。
- `rows` 按 accepted_count 降序、penalty_minutes 升序、最后按 display_name 升序稳定排序。
- `cells` 按 `contest_problems.sort_order` 排列。
- cell `status` 取值：`none`、`attempted`、`accepted`、`frozen`。
- live/final 视图中，`attempted` 显示未 AC 尝试次数，`accepted` 显示首次 AC 时间和 AC 前错误次数。
- frozen 视图中，封榜前已判定结果正常显示；封榜后产生且不应公开的提交汇总为 `status = 'frozen'` 和 `frozen_attempts`，不暴露 accepted_at、last_submission_id 和真实结果。
- `view=final` 在 `now < end_at` 时返回 `403 contest.scoreboard_hidden`。
- `view=frozen` 在 `now < freeze_at` 时返回 `400 invalid_argument`，提示 frozen view is not available before freeze time。

## 部署设计

Docker Compose 第一版包含：

```text
soj-api
soj-worker
soj-migrate
postgres
redis
minio
judge0-server
judge0-worker
judge0-db
judge0-redis
```

配置：

- `deploy/config.example.yaml` 提供非敏感示例。
- v2 canonical Compose 文件为 `deploy/docker-compose.yaml`；根目录旧 `docker-compose.yaml` 在 v2 切换默认入口时删除或改为指向 v2 文档。
- 真实 secret 通过环境变量或本地私有配置注入。
- 仓库不提交真实 JWT secret、数据库密码、对象存储密钥。
- Dockerfile 使用 Go 1.26.x builder。

健康检查：

- API 提供 `/healthz` 和 `/readyz`。
- worker 暴露轻量 HTTP health endpoint，默认监听内部端口，例如 `:9090`。
- API 和 worker 的 `/healthz` 只表示进程存活。
- API 和 worker 的 `/readyz` 检查 PostgreSQL、Redis、对象存储和 JudgeEngine adapter。
- Docker Compose healthcheck 使用 `/readyz`，依赖服务未就绪时容器保持 unhealthy。

## 可观测性

- 每个请求生成 `request_id`。
- worker 每个任务生成或继承 `trace_id`。
- 结构化日志包含模块、request id、actor id、submission id、task id。
- 指标至少包含 API 请求数、延迟、错误数、队列 pending 数、worker 成功/失败数、判题耗时。
- 错误日志不输出 secret、token、完整源码、完整测试点内容。

## 测试策略

质量门禁：

- `go test ./...` 必须通过。
- migration 在空库上完整执行。
- Docker Compose 环境能启动 API、worker、PostgreSQL、Redis、MinIO 和 Judge adapter。

核心单元测试：

- JWT 登录、刷新、吊销。
- 角色 + owner 权限判断。
- 题目创建、发布、测试点 ready 检查。
- 正式提交创建 submission 和 judge task。
- worker 幂等消费。
- 比赛封榜和榜单排序。

集成测试：

- PostgreSQL 事务和 sqlc queries。
- Redis Stream 消费、ack、pending reclaim、dead-letter。
- MinIO 对象写入和读取。
- JudgeEngine fake adapter 映射。

HTTP handler 测试：

- 未登录。
- 权限不足。
- 参数错误。
- 成功响应格式。
- OpenAPI 中声明的核心 endpoint。

## 文档交付

- `api/openapi.yaml`：前端联调主契约。
- `docs/v2-architecture.md`：架构、服务边界、依赖和数据流。
- `docs/v2-api-guide.md`：面向前端的接口使用说明。
- `docs/v2-deploy.md`：Docker Compose 部署、配置、密钥和依赖启动说明。
- `docs/v2-worker.md`：Redis Stream、重试、死信和 worker 扩容说明。

## 实施阶段

### 阶段 1：v2 基础骨架

- 新建 `cmd/soj-api`、`cmd/soj-worker`、`cmd/soj-migrate`。
- 建立 typed config、logger、PostgreSQL、Redis、MinIO、Gin 基础设施。
- 建立 migration 工具和初始 schema。
- 建立 OpenAPI 文件骨架。

### 阶段 2：认证与用户

- 实现用户注册、登录、refresh、logout、`GET /me`。
- 实现 JWT access token 和 refresh token hash 存储。
- 实现 `Actor` 和权限辅助。

### 阶段 3：题目与对象存储

- 实现题目、标签、题面版本、测试点集合。
- 实现 MinIO/S3 adapter。
- 实现题目发布约束：必须存在 ready testcase set。

### 阶段 4：提交与判题 worker

- 实现 submissions、artifacts、judge_tasks。
- 实现 Redis Stream TaskQueue。
- 实现 JudgeEngine fake 和 Judge0 adapter。
- 实现正式提交异步和自测短等待。
- 实现 worker 幂等和重试。

### 阶段 5：ACM 比赛

- 实现比赛、题集、报名、赛时提交校验。
- 实现封榜、榜单聚合和 snapshot。
- 覆盖核心榜单测试。

### 阶段 6：部署、文档与旧代码替换

- 完成 Docker Compose。
- 完成 OpenAPI 和前端接口说明。
- 完成部署和 worker 文档。
- v2 通过门禁后切换默认入口。
- 清理旧实现。

## 验收标准

- 空仓库依赖环境下，按 `docs/v2-deploy.md` 可以通过 Docker Compose 启动完整后端环境。
- `go test ./...` 通过。
- migration 可在空 PostgreSQL 数据库上执行完成。
- OpenAPI 覆盖认证、题目、提交、自测、比赛、管理语言核心接口。
- 正式提交 API 能创建 queued submission，并由 worker 消费到终态。
- 重复消费同一判题任务不会重复更新比赛榜单或罚时。
- Redis Stream pending 消息可被新 worker reclaim。
- 超过重试次数的任务进入 dead-letter，并将 submission 标记为 `system_error`。
- 基础 ACM 比赛榜单符合 accepted 数量优先、罚时升序、封榜视图规则。
- 仓库中不提交真实 secret。
