#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILES="${COMPOSE_FILES:-${COMPOSE_FILE:-deploy/docker-compose.yaml:deploy/docker-compose.docker-runner.yaml}}"
API_URL="${API_URL:-http://localhost:8080}"
WORKER_URL="${WORKER_URL:-http://localhost:8081}"
JUDGE_AGENT_URL="${JUDGE_AGENT_URL:-http://localhost:8082}"
RUN_ID="${RUN_ID:-capacity-$(date +%s)-$$}"
SLOTS_RAW="${SOJ_CAPACITY_SLOTS:-1 2 4 8 16}"
SUBMISSIONS_PER_SLOT="${SOJ_CAPACITY_SUBMISSIONS_PER_SLOT:-4}"
SUBMISSIONS_MIN="${SOJ_CAPACITY_SUBMISSIONS_MIN:-4}"
SUBMISSIONS_MAX="${SOJ_CAPACITY_SUBMISSIONS_MAX:-64}"
STARTUP_SAMPLES="${SOJ_CAPACITY_STARTUP_SAMPLES:-5}"
TIMEOUT_SECONDS="${SOJ_CAPACITY_TIMEOUT_SECONDS:-240}"
CAPACITY_TIME_LIMIT_MS="${SOJ_CAPACITY_TIME_LIMIT_MS:-10000}"
CAPACITY_MEMORY_LIMIT_KB="${SOJ_CAPACITY_MEMORY_LIMIT_KB:-262144}"
SKIP_BUILD="${SOJ_CAPACITY_SKIP_BUILD:-0}"
SOJ_ENV="${SOJ_ENV:-local}"
SOJ_DOCKER_RUNNER_IMAGE_GO="${SOJ_DOCKER_RUNNER_IMAGE_GO:-ghcr.io/sparklyi/soj-runner-go:main}"
SOJ_DOCKER_RUNNER_IMAGE_CPP17="${SOJ_DOCKER_RUNNER_IMAGE_CPP17:-ghcr.io/sparklyi/soj-runner-cpp17:main}"
SOJ_DOCKER_RUNNER_WORKDIR="${SOJ_DOCKER_RUNNER_WORKDIR:-/tmp/soj-runner-work}"
SOJ_DOCKER_RUNNER_RUNTIME="${SOJ_DOCKER_RUNNER_RUNTIME:-}"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

compose() {
  local args=()
  local files=()
  IFS=':' read -r -a files <<< "$COMPOSE_FILES"
  for file in "${files[@]}"; do
    args+=(-f "$file")
  done
  docker compose "${args[@]}" "$@"
}

wait_http() {
  local url="$1"
  for _ in $(seq 1 60); do
    if curl -fsS "$url" >/dev/null; then
      return 0
    fi
    sleep 1
  done
  curl -fsS "$url" >/dev/null
}

psql_scalar() {
  local sql="$1"
  compose exec -T postgres psql -U soj -d soj -Atc "$sql" | tr -d '\r'
}

psql_fields() {
  local sql="$1"
  compose exec -T postgres psql -U soj -d soj -At -F $'\t' -c "$sql" | tr -d '\r'
}

api_json() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  if [[ -n "$body" ]]; then
    curl -fsS -X "$method" "$API_URL$path" \
      -H "authorization: Bearer $TOKEN" \
      -H "content-type: application/json" \
      -d "$body"
  else
    curl -fsS -X "$method" "$API_URL$path" \
      -H "authorization: Bearer $TOKEN"
  fi
}

now_ms() {
  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import time; print(int(time.time() * 1000))'
  else
    printf '%s000\n' "$(date +%s)"
  fi
}

percentile_file() {
  local file="$1"
  local percentile="$2"
  local count index
  count="$(wc -l < "$file" | tr -d '[:space:]')"
  if [[ "$count" == "0" ]]; then
    printf '0'
    return
  fi
  index=$(( (percentile * count + 99) / 100 ))
  if (( index < 1 )); then
    index=1
  fi
  sort -n "$file" | sed -n "${index}p"
}

metric_sum() {
  local url="$1"
  local metric="$2"
  curl -fsS "$url/metrics" | awk -v metric="$metric" '$0 ~ "^" metric {sum += $NF} END {printf "%.0f", sum + 0}'
}

agent_memory_mib() {
  local cid usage
  cid="$(compose ps -q judge-agent | head -n 1)"
  if [[ -z "$cid" ]]; then
    printf '0'
    return
  fi
  usage="$(docker stats --no-stream --format '{{.MemUsage}}' "$cid" 2>/dev/null | awk '{print $1}')"
  awk -v value="$usage" '
    BEGIN {
      if (value ~ /GiB$/) { sub(/GiB$/, "", value); printf "%.2f", value * 1024; exit }
      if (value ~ /MiB$/) { sub(/MiB$/, "", value); printf "%.2f", value; exit }
      if (value ~ /KiB$/) { sub(/KiB$/, "", value); printf "%.2f", value / 1024; exit }
      if (value ~ /kB$/) { sub(/kB$/, "", value); printf "%.2f", value / 1024; exit }
      if (value ~ /MB$/) { sub(/MB$/, "", value); printf "%.2f", value; exit }
      if (value ~ /GB$/) { sub(/GB$/, "", value); printf "%.2f", value * 1024; exit }
      if (value ~ /B$/) { sub(/B$/, "", value); printf "%.2f", value / 1024 / 1024; exit }
      printf "0"
    }'
}

float_gt() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left > right) }'
}

queue_oldest_pending_age_s() {
  psql_scalar "
select coalesce(round(extract(epoch from (now() - min(created_at)))::numeric, 2), 0)
from judge_tasks
where status in ('pending', 'dispatching', 'dispatched', 'running');
"
}

check_runner() {
  if [[ -n "$SOJ_DOCKER_RUNNER_RUNTIME" ]]; then
    SOJ_DOCKER_RUNNER_RUNTIME="$SOJ_DOCKER_RUNNER_RUNTIME" \
    SOJ_DOCKER_RUNNER_IMAGE_GO="$SOJ_DOCKER_RUNNER_IMAGE_GO" \
    SOJ_DOCKER_RUNNER_IMAGE_CPP17="$SOJ_DOCKER_RUNNER_IMAGE_CPP17" \
      ./scripts/dev/check-docker-runner.sh "$SOJ_DOCKER_RUNNER_RUNTIME"
  else
    SOJ_DOCKER_RUNNER_IMAGE_GO="$SOJ_DOCKER_RUNNER_IMAGE_GO" \
    SOJ_DOCKER_RUNNER_IMAGE_CPP17="$SOJ_DOCKER_RUNNER_IMAGE_CPP17" \
      ./scripts/dev/check-docker-runner.sh
  fi
}

boot_stack_for_slots() {
  local slots="$1"
  export SOJ_ENV
  export SOJ_DOCKER_RUNNER_RUNTIME
  export SOJ_DOCKER_RUNNER_IMAGE_GO
  export SOJ_DOCKER_RUNNER_IMAGE_CPP17
  export SOJ_DOCKER_RUNNER_WORKDIR
  export SOJ_JUDGE_PARALLELISM="$slots"
  export SOJ_JUDGE_LANGUAGE_SLOTS="go=$slots,cpp17=$slots"
  export SOJ_JUDGE_MAX_BATCH="$slots"

  if [[ "${STACK_BOOTED:-0}" == "0" ]]; then
    if [[ "$SKIP_BUILD" == "1" ]]; then
      compose up -d
    else
      compose up --build -d
    fi
    STACK_BOOTED=1
  else
    compose up -d --no-deps --force-recreate judge-agent
  fi
  wait_http "$API_URL/readyz"
  wait_http "$WORKER_URL/readyz"
  wait_http "$JUDGE_AGENT_URL/readyz"
}

setup_problem() {
  LANG_ID="$(psql_scalar "select id from languages where engine = 'soj-agent' and engine_language_id = 'go' and enabled = true order by id limit 1;")"
  if [[ -z "$LANG_ID" ]]; then
    echo "go language seed was not found" >&2
    exit 1
  fi

  local register_response
  register_response="$(curl -fsS -X POST "$API_URL/api/v1/auth/register" \
    -H "content-type: application/json" \
    -d "{\"email\":\"$RUN_ID@example.com\",\"username\":\"$RUN_ID\",\"password\":\"Passw0rd!\",\"device_id\":\"capacity\"}")"
  TOKEN="$(jq -r '.data.access_token' <<< "$register_response")"
  if [[ -z "$TOKEN" || "$TOKEN" == "null" ]]; then
    echo "failed to get access token" >&2
    exit 1
  fi

  local problem_response tmp_dir sha
  problem_response="$(api_json POST /api/v1/problems "{\"title\":\"Capacity A+B $RUN_ID\",\"slug\":\"capacity-ab-$RUN_ID\",\"difficulty\":\"easy\",\"visibility\":\"public\",\"time_limit_ms\":$CAPACITY_TIME_LIMIT_MS,\"memory_limit_kb\":$CAPACITY_MEMORY_LIMIT_KB}")"
  PROBLEM_ID="$(jq -r '.data.id' <<< "$problem_response")"
  api_json POST "/api/v1/problems/$PROBLEM_ID/statement" '{"title":"Capacity A+B","description":"Add two numbers","input_description":"two integers","output_description":"sum","samples":[{"input":"1 1\n","output":"2\n"}]}' >/dev/null

  tmp_dir="$TMP_ROOT/cases"
  mkdir -p "$tmp_dir"
  printf '1 1\n' > "$tmp_dir/input1.txt"
  printf '2\n' > "$tmp_dir/output1.txt"
  (cd "$tmp_dir" && zip -q cases.zip input1.txt output1.txt)
  sha="$(shasum -a 256 "$tmp_dir/cases.zip" | awk '{print $1}')"

  curl -fsS -X POST "$API_URL/api/v1/problems/$PROBLEM_ID/testcase-sets" \
    -H "authorization: Bearer $TOKEN" \
    -F "archive=@$tmp_dir/cases.zip;type=application/zip" \
    -F "case_count=1" \
    -F "checksum_sha256=$sha" >/dev/null

  api_json PATCH "/api/v1/problems/$PROBLEM_ID" '{"status":"published"}' >/dev/null
}

source_code() {
  local index="$1"
  cat <<SRC
package main

import "fmt"

func main() {
	var a, b int
	fmt.Scan(&a, &b)
	fmt.Println(a + b)
}

// capacity submission $index
SRC
}

submit_one() {
  local index="$1"
  local ids_file="$2"
  local payload response submission_id source
  source="$(source_code "$index")"
  payload="$(jq -cn --argjson problem_id "$PROBLEM_ID" --argjson language_id "$LANG_ID" --arg source "$source" '{problem_id:$problem_id,language_id:$language_id,source_code:$source}')"
  response="$(api_json POST /api/v1/submissions "$payload")"
  submission_id="$(jq -r '.data.id' <<< "$response")"
  if [[ -z "$submission_id" || "$submission_id" == "null" ]]; then
    echo "failed to create capacity submission $index" >&2
    exit 1
  fi
  printf '%s\n' "$submission_id" >> "$ids_file"
}

dump_submission_statuses() {
  local ids_csv="$1"
  compose exec -T postgres psql -U soj -d soj -c "
select s.id, s.status as submission_status, jt.status as task_status, ja.status as attempt_status, s.submitted_at, s.judged_at, ja.error_class, ja.error_message
from submissions s
left join judge_tasks jt on jt.submission_id = s.id
left join judge_attempts ja on ja.submission_id = s.id
where s.id in ($ids_csv)
order by s.id;
" >&2
}

wait_for_submissions() {
  local ids_csv="$1"
  local total="$2"
  local deadline=$((SECONDS + TIMEOUT_SECONDS))
  QUEUE_AGE_MAX="0"
  AGENT_MEM_PEAK="$AGENT_MEM_START"

  while (( SECONDS < deadline )); do
    local row terminal_count pending_count failed_count queue_age mem
    row="$(psql_fields "
select
  count(*) filter (where status in ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled')),
  count(*) filter (where status not in ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled')),
  count(*) filter (where status not in ('queued', 'running', 'accepted'))
from submissions
where id in ($ids_csv);
")"
    IFS=$'\t' read -r terminal_count pending_count failed_count <<< "$row"
    queue_age="$(queue_oldest_pending_age_s)"
    mem="$(agent_memory_mib)"
    if float_gt "$queue_age" "$QUEUE_AGE_MAX"; then
      QUEUE_AGE_MAX="$queue_age"
    fi
    if float_gt "$mem" "$AGENT_MEM_PEAK"; then
      AGENT_MEM_PEAK="$mem"
    fi
    if [[ "$terminal_count" == "$total" ]]; then
      return 0
    fi
    sleep 1
  done

  echo "capacity submissions did not finish within ${TIMEOUT_SECONDS}s" >&2
  dump_submission_statuses "$ids_csv"
  exit 1
}

measure_container_startup() {
  local out_file="$1"
  local docker_args=(run --rm)
  local start end
  : > "$out_file"
  if [[ -n "$SOJ_DOCKER_RUNNER_RUNTIME" ]]; then
    docker_args+=(--runtime "$SOJ_DOCKER_RUNNER_RUNTIME")
  fi
  for _ in $(seq 1 "$STARTUP_SAMPLES"); do
    start="$(now_ms)"
    docker "${docker_args[@]}" \
      --network none \
      --read-only \
      --cap-drop ALL \
      --security-opt no-new-privileges \
      --user 1000:1000 \
      --pids-limit 32 \
      --memory 128m \
      --tmpfs /tmp:rw,nosuid,nodev,noexec,size=32m \
      "$SOJ_DOCKER_RUNNER_IMAGE_GO" sh -lc 'true' >/dev/null
    end="$(now_ms)"
    printf '%s\n' "$((end - start))" >> "$out_file"
  done
}

run_slot_benchmark() {
  local slots="$1"
  local total ids_file startup_file ids_csv errors_before errors_after cleanup_before cleanup_after
  local p95_startup p99_startup row completed accepted submissions_per_min p95_latency p99_latency p95_attempt p99_attempt attempt_memory_max
  local agent_mem_end queue_age_end sandbox_errors_delta cleanup_failures_delta

  total=$((slots * SUBMISSIONS_PER_SLOT))
  if (( total < SUBMISSIONS_MIN )); then
    total="$SUBMISSIONS_MIN"
  fi
  if (( total > SUBMISSIONS_MAX )); then
    total="$SUBMISSIONS_MAX"
  fi

  ids_file="$TMP_ROOT/submissions-$slots.txt"
  startup_file="$TMP_ROOT/startup-$slots.txt"
  : > "$ids_file"
  errors_before="$(metric_sum "$JUDGE_AGENT_URL" "soj_sandbox_backend_errors_total")"
  cleanup_before="$(metric_sum "$JUDGE_AGENT_URL" "soj_sandbox_cleanup_failures_total")"
  AGENT_MEM_START="$(agent_memory_mib)"

  measure_container_startup "$startup_file"
  p95_startup="$(percentile_file "$startup_file" 95)"
  p99_startup="$(percentile_file "$startup_file" 99)"

  for index in $(seq 1 "$total"); do
    submit_one "$index" "$ids_file" &
  done
  wait

  ids=()
  while IFS= read -r id; do
    ids+=("$id")
  done < "$ids_file"
  if (( ${#ids[@]} != total )); then
    echo "created ${#ids[@]} submissions, want $total" >&2
    exit 1
  fi
  ids_csv="$(IFS=,; echo "${ids[*]}")"
  wait_for_submissions "$ids_csv" "$total"

  row="$(psql_fields "
with selected as (
  select
    s.id,
    s.status,
    s.submitted_at,
    s.judged_at,
    ja.started_at as attempt_started_at,
    ja.finished_at as attempt_finished_at,
    ja.memory_kb
  from submissions s
  join judge_attempts ja on ja.submission_id = s.id
  where s.id in ($ids_csv)
)
select
  count(*),
  count(*) filter (where status = 'accepted'),
  round((60.0 * count(*) / greatest(extract(epoch from (max(judged_at) - min(submitted_at))), 0.001))::numeric, 2),
  round(coalesce(percentile_cont(0.95) within group (order by extract(epoch from (judged_at - submitted_at)) * 1000), 0)::numeric, 2),
  round(coalesce(percentile_cont(0.99) within group (order by extract(epoch from (judged_at - submitted_at)) * 1000), 0)::numeric, 2),
  round(coalesce(percentile_cont(0.95) within group (order by extract(epoch from (attempt_finished_at - attempt_started_at)) * 1000), 0)::numeric, 2),
  round(coalesce(percentile_cont(0.99) within group (order by extract(epoch from (attempt_finished_at - attempt_started_at)) * 1000), 0)::numeric, 2),
  coalesce(max(memory_kb), 0)
from selected;
")"
  IFS=$'\t' read -r completed accepted submissions_per_min p95_latency p99_latency p95_attempt p99_attempt attempt_memory_max <<< "$row"

  agent_mem_end="$(agent_memory_mib)"
  queue_age_end="$(queue_oldest_pending_age_s)"
  errors_after="$(metric_sum "$JUDGE_AGENT_URL" "soj_sandbox_backend_errors_total")"
  cleanup_after="$(metric_sum "$JUDGE_AGENT_URL" "soj_sandbox_cleanup_failures_total")"
  sandbox_errors_delta=$((errors_after - errors_before))
  cleanup_failures_delta=$((cleanup_after - cleanup_before))

  printf 'capacity slots=%s submissions=%s accepted=%s submissions_per_min=%s p95_latency_ms=%s p99_latency_ms=%s p95_attempt_ms=%s p99_attempt_ms=%s container_startup_p95_ms=%s container_startup_p99_ms=%s attempt_memory_max_kb=%s agent_memory_mib_start=%s agent_memory_mib_peak=%s agent_memory_mib_end=%s queue_oldest_pending_age_max_s=%s queue_oldest_pending_age_end_s=%s sandbox_errors_delta=%s cleanup_failures_delta=%s\n' \
    "$slots" "$completed" "$accepted" "$submissions_per_min" "$p95_latency" "$p99_latency" "$p95_attempt" "$p99_attempt" "$p95_startup" "$p99_startup" "$attempt_memory_max" "$AGENT_MEM_START" "$AGENT_MEM_PEAK" "$agent_mem_end" "$QUEUE_AGE_MAX" "$queue_age_end" "$sandbox_errors_delta" "$cleanup_failures_delta"

  if [[ "$completed" != "$total" || "$accepted" != "$total" ]]; then
    echo "capacity benchmark expected all submissions accepted for slots=$slots" >&2
    dump_submission_statuses "$ids_csv"
    exit 1
  fi
}

need awk
need curl
need date
need docker
need grep
need jq
need sed
need shasum
need sort
need wc
need zip

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

mkdir -p "$SOJ_DOCKER_RUNNER_WORKDIR"
check_runner

STACK_BOOTED=0
PROBLEM_ID=""
TOKEN=""
LANG_ID=""

SLOTS_LIST="$(tr ',' ' ' <<< "$SLOTS_RAW")"
for slots in $SLOTS_LIST; do
  if ! [[ "$slots" =~ ^[0-9]+$ ]] || (( slots <= 0 )); then
    echo "invalid slot value: $slots" >&2
    exit 1
  fi
  boot_stack_for_slots "$slots"
  if [[ -z "$PROBLEM_ID" ]]; then
    setup_problem
  fi
  run_slot_benchmark "$slots"
done
