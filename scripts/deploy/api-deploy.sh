#!/usr/bin/env bash
# 部署一个后端 v* 版本：把 checkout 同步到该 tag（compose 文件跟着走），
# 从 GitHub Release 下载 docker save 的镜像包，docker load 后重打 :latest，
# compose up -d；健康检查失败自动回退到原镜像。
# 服务器上不做任何构建（2G 内存约束）。构建由 SOJ 仓库的 deploy-backend.yml 完成。
# 用法: api-deploy.sh <tag>   例如 api-deploy.sh v1.3.2
set -euo pipefail

TAG="${1:?usage: api-deploy.sh <tag>}"
case "$TAG" in
  v[0-9]*) ;;
  *) echo "[api-deploy] refusing non v* tag: $TAG" >&2; exit 2 ;;
esac

REPO="sparklyi/SOJ"
SVCS="api worker judge-agent migrate"
BACKEND_DIR="/opt/soj/backend"
COMPOSE_DIR="/opt/soj/backend/deploy"
STATE_DIR="/opt/soj/state"

compose() {
  (cd "$COMPOSE_DIR" && docker compose --env-file /opt/soj/.env \
    -f docker-compose.yaml -f docker-compose.prod.yaml "$@")
}

mkdir -p "$STATE_DIR"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# 记录当前 :latest 镜像 ID，用于回退
: > "$TMP/prev-ids"
for SVC in $SVCS; do
  ID="$(docker image inspect "soj-$SVC:latest" --format '{{.Id}}' 2>/dev/null || true)"
  [ -n "$ID" ] && echo "$SVC $ID" >> "$TMP/prev-ids"
done

# 记录当前 checkout 位置，回退时和镜像一起还原
PREV_REF="$(git -C "$BACKEND_DIR" rev-parse HEAD)"

# compose 文件来自这个 checkout，镜像换版本了它也得跟着换。不同步的话就是
# 「新镜像配旧 compose」——缺配置时不报错，只是行为不对。
# 故意不带 --force：checkout 脏了就失败退出，而不是静默丢掉服务器上的手改。
echo "[api-deploy] syncing $BACKEND_DIR to $TAG"
git -C "$BACKEND_DIR" fetch --tags --force --quiet origin
git -C "$BACKEND_DIR" checkout --quiet "$TAG"

for SVC in $SVCS; do
  URL="https://github.com/${REPO}/releases/download/${TAG}/soj-${SVC}-${TAG}.tar.gz"
  echo "[api-deploy] downloading $URL"
  curl -fsSL --retry 3 --retry-delay 5 -o "$TMP/$SVC.tar.gz" "$URL"
  docker load -i "$TMP/$SVC.tar.gz"
  docker tag "soj-$SVC:$TAG" "soj-$SVC:latest"
done

echo "[api-deploy] compose up -d ($TAG)"
compose up -d

ok=0
for _ in $(seq 1 90); do
  if curl -fsS -o /dev/null http://127.0.0.1:8080/readyz; then ok=1; break; fi
  sleep 2
done

if [ "$ok" != 1 ]; then
  echo "[api-deploy] health check failed for $TAG, rolling back images and $BACKEND_DIR" >&2
  while read -r SVC ID; do
    docker tag "$ID" "soj-$SVC:latest"
  done < "$TMP/prev-ids"
  git -C "$BACKEND_DIR" checkout --quiet "$PREV_REF"
  compose up -d
  exit 1
fi

echo "$TAG" > "$STATE_DIR/api-current"
echo "[api-deploy] $TAG is live"

# 每个服务只保留最近 3 个版本镜像
for SVC in $SVCS; do
  docker images "soj-$SVC" --format '{{.Tag}}' | grep -E '^v[0-9]' | sort -V | tail -n +4 \
    | while read -r t; do docker rmi "soj-$SVC:$t" >/dev/null 2>&1 || true; done
done
