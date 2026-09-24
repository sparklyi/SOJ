#!/usr/bin/env bash
# 生产 compose「渲染结果」断言。
#
# 为什么必须查渲染结果而不是源文件：seed 任务的测试语言守卫如果写成 compose 插值式
# （单个 $ 加花括号），compose 会在**创建容器那一刻**就把值内联进脚本文本 —— 当时变量
# 未设置，于是内联成常量 "true"。此后 docker-compose.prod.yaml 里改 environment 完全
# 无效（容器 env 是 false，但没人读它）。源文件看起来毫无问题，只有渲染结果暴露它。
# 2026-09-21 生产库被种上 Fake Accepted 就是这个原因。
#
# 用法：scripts/ci/check-prod-compose.sh
# 需要 docker compose；未设置的变量会用占位值（仅用于渲染，不连任何服务）。
set -euo pipefail

cd "$(dirname "$0")/../.."

export SOJ_ENV="${SOJ_ENV:-prod}"
export SOJ_POSTGRES_PASSWORD="${SOJ_POSTGRES_PASSWORD:-ci-postgres-password}"
export SOJ_DATABASE_DSN="${SOJ_DATABASE_DSN:-postgres://soj:ci-postgres-password@postgres:5432/soj?sslmode=disable}"
export SOJ_STORAGE_ACCESS_KEY="${SOJ_STORAGE_ACCESS_KEY:-ci-storage-user}"
export SOJ_STORAGE_SECRET_KEY="${SOJ_STORAGE_SECRET_KEY:-ci-storage-secret}"
export SOJ_JWT_SECRET="${SOJ_JWT_SECRET:-ci-jwt-secret}"
export SOJ_DOCKER_RUNNER_IMAGE_GO="${SOJ_DOCKER_RUNNER_IMAGE_GO:-ghcr.io/sparklyi/soj-runner-go:main}"
export SOJ_DOCKER_RUNNER_IMAGE_CPP17="${SOJ_DOCKER_RUNNER_IMAGE_CPP17:-ghcr.io/sparklyi/soj-runner-cpp17:main}"

fail() {
  echo "check-prod-compose: $1" >&2
  exit 1
}

rendered="$(docker compose \
  -f deploy/docker-compose.yaml \
  -f deploy/docker-compose.prod.yaml config)"

# 只取 seed 段，避免匹配到其他服务。
seed="$(printf '%s\n' "$rendered" | awk '/^  seed:/{found=1; next} found && /^  [a-z]/{exit} found')"
[ -n "$seed" ] || fail "渲染结果里找不到 seed 段"

# 1) 生产必须把开关置为 false：守卫即使正确求值，也得有个 false 可读。
grep -Eq 'SOJ_SEED_FAKE_LANGUAGE: *"?false"?' <<<"$seed" \
  || fail "seed 服务在生产渲染下 SOJ_SEED_FAKE_LANGUAGE 不是 false"

# 2) 守卫必须保留变量引用（运行时求值）。渲染文本里 $$ 显示为 $$，不同 compose 版本
#    也可能显示为单个 $，两种都放行。
grep -Eq 'if \[ "\$\$?\{SOJ_SEED_FAKE_LANGUAGE:-true\}" = "true" \]' <<<"$seed" \
  || fail "seed 守卫没有保留变量引用，可能已被 compose 在创建容器时内联"

# 3) 明确拒绝已被内联成常量的形态（就是 2026-09-21 生产出事的样子）。
if grep -q '\[ "true" = "true" \]' <<<"$seed"; then
  fail 'seed 守卫已被内联成常量 "true"：生产 compose 的 environment 将完全失效'
fi

echo "check-prod-compose: ok（seed 守卫在运行时求值，生产开关为 false）"
