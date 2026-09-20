#!/usr/bin/env bash
# 部署一个后端 v* 版本：从 GitHub Release 下载 docker save 的镜像包，
# docker load 后重打 :latest，compose up -d；健康检查失败自动回退到原镜像。
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
  echo "[api-deploy] health check failed for $TAG, rolling back images" >&2
  while read -r SVC ID; do
    docker tag "$ID" "soj-$SVC:latest"
  done < "$TMP/prev-ids"
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
