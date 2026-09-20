#!/usr/bin/env bash
# 轮询 GitHub 上 sparklyi/SOJ 的最新 v* tag，发现有新版本就调用 api-deploy.sh。
# 由 soj-api-deploy.timer 每 5 分钟拉起；flock 防并发。
# 只匹配 v<数字>*，不会碰到前端的 web-v* tag。
set -euo pipefail

exec 9>"/run/soj-api-deploy.lock"
flock -n 9 || exit 0

STATE="/opt/soj/state/api-current"
LATEST="$(git ls-remote --tags --refs https://github.com/sparklyi/SOJ.git 'refs/tags/v*' \
  | awk -F/ '{print $NF}' | grep -E '^v[0-9]' | sort -V | tail -1)"

[ -n "$LATEST" ] || exit 0
CURRENT="$(cat "$STATE" 2>/dev/null || echo none)"
[ "$LATEST" = "$CURRENT" ] && exit 0

echo "[api-deploy-check] found new tag $LATEST (current: $CURRENT)"
exec /opt/soj/bin/api-deploy.sh "$LATEST"
