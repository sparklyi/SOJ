#!/usr/bin/env bash
set -euo pipefail

RUNTIME="${SOJ_DOCKER_RUNNER_RUNTIME:-${1:-}}"
GO_IMAGE="${SOJ_DOCKER_RUNNER_IMAGE_GO:-ghcr.io/sparklyi/soj-runner-go:main}"
CPP_IMAGE="${SOJ_DOCKER_RUNNER_IMAGE_CPP17:-ghcr.io/sparklyi/soj-runner-cpp17:main}"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

if [[ -n "$RUNTIME" ]]; then
  if ! docker info --format '{{json .Runtimes}}' | grep -q "\"$RUNTIME\""; then
    echo "docker runtime $RUNTIME is not registered" >&2
    exit 1
  fi
fi

check_image() {
  local image="$1"
  if ! docker image inspect "$image" >/dev/null 2>&1; then
    echo "runner image $image was not found; run: make runner-images-pull" >&2
    echo "or use make runner-images-build while developing runner Dockerfiles" >&2
    exit 1
  fi
}

check_noop() {
  local image="$1"
  local docker_args=(run --rm)
  if [[ -n "$RUNTIME" ]]; then
    docker_args+=(--runtime "$RUNTIME")
  fi
  docker "${docker_args[@]}" \
    --network none \
    --read-only \
    --cap-drop ALL \
    --security-opt no-new-privileges \
    --user 1000:1000 \
    --pids-limit 32 \
    --memory 128m \
    --tmpfs /tmp:rw,nosuid,nodev,noexec,size=32m \
    "$image" sh -lc 'test "$(id -u)" = "1000" && test ! -e /var/run/docker.sock'
}

need docker
check_image "$GO_IMAGE"
check_image "$CPP_IMAGE"
check_noop "$GO_IMAGE"
check_noop "$CPP_IMAGE"

if [[ -n "$RUNTIME" ]]; then
  echo "docker runner check ok with runtime=$RUNTIME go_image=$GO_IMAGE cpp_image=$CPP_IMAGE"
else
  echo "docker runner check ok with default docker runtime go_image=$GO_IMAGE cpp_image=$CPP_IMAGE"
fi
