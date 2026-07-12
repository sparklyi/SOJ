#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/docker-compose.yaml}"
COMPOSE_FILES="${COMPOSE_FILES:-$COMPOSE_FILE}"
API_URL="${API_URL:-http://localhost:8080}"
RUN_ID="${RUN_ID:-$(date +%s)-$$}"
SMOKE_REAL_JUDGE="${SMOKE_REAL_JUDGE:-0}"

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
  for _ in $(seq 1 30); do
    if curl -fsS "$url" >/dev/null; then
      return 0
    fi
    sleep 1
  done
  curl -fsS "$url" >/dev/null
}

require_metric() {
  local url="$1"
  local metric="$2"
  local body
  if ! body="$(curl -fsS "$url/metrics")" || ! grep -q "$metric" <<<"$body"; then
    echo "metric $metric was not found at $url/metrics" >&2
    exit 1
  fi
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

need curl
need docker
need grep
need jq
need shasum
need zip

wait_http "$API_URL/readyz"
wait_http "${WORKER_URL:-http://localhost:8081}/readyz"
wait_http "${JUDGE_AGENT_URL:-http://localhost:8082}/readyz"
require_metric "$API_URL" "soj_http_requests_total"
require_metric "${JUDGE_AGENT_URL:-http://localhost:8082}" "soj_http_requests_total"

if [[ "$SMOKE_REAL_JUDGE" == "1" ]]; then
  LANG_ID="$(compose exec -T postgres psql -U soj -d soj -Atc "select id from languages where engine = 'soj-agent' and engine_language_id = 'go' and enabled = true order by id limit 1;")"
  if [[ -z "$LANG_ID" ]]; then
    echo "go language seed was not found" >&2
    exit 1
  fi
  SOURCE_CODE="$(cat <<'SRC'
package main

import "fmt"

func main() {
	var a, b int
	fmt.Scan(&a, &b)
	fmt.Println(a + b)
}
SRC
)"
  WRONG_SOURCE_CODE="$(cat <<'SRC'
package main

import "fmt"

func main() {
	fmt.Println(0)
}
SRC
)"
else
  LANG_ID="$(compose exec -T postgres psql -U soj -d soj -Atc "select id from languages where engine = 'fake' and engine_language_id = 'accepted' and enabled = true order by id limit 1;")"
  if [[ -z "$LANG_ID" ]]; then
    echo "fake language seed was not found" >&2
    exit 1
  fi
  SOURCE_CODE="print(2)"
  WRONG_SOURCE_CODE="print(0)"
fi

REGISTER_RESPONSE="$(curl -fsS -X POST "$API_URL/api/v1/auth/register" \
  -H "content-type: application/json" \
  -d "{\"email\":\"smoke-$RUN_ID@example.com\",\"username\":\"smoke-$RUN_ID\",\"password\":\"Passw0rd!\",\"device_id\":\"smoke\"}")"
TOKEN="$(jq -r '.data.access_token' <<<"$REGISTER_RESPONSE")"
if [[ -z "$TOKEN" || "$TOKEN" == "null" ]]; then
  echo "failed to get access token" >&2
  exit 1
fi

PROBLEM_RESPONSE="$(api_json POST /api/v1/problems "{\"title\":\"Smoke A+B $RUN_ID\",\"slug\":\"smoke-ab-$RUN_ID\",\"difficulty\":\"easy\",\"visibility\":\"public\",\"time_limit_ms\":1000,\"memory_limit_kb\":65536}")"
PROBLEM_ID="$(jq -r '.data.id' <<<"$PROBLEM_RESPONSE")"

api_json POST "/api/v1/problems/$PROBLEM_ID/statement" '{"title":"Smoke A+B","description":"Add two numbers","input_description":"two integers","output_description":"sum","samples":[{"input":"1 1\n","output":"2\n"}]}' >/dev/null

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
printf '1 1\n' > "$TMP_DIR/input1.txt"
printf '2\n' > "$TMP_DIR/output1.txt"
(cd "$TMP_DIR" && zip -q cases.zip input1.txt output1.txt)
SHA="$(shasum -a 256 "$TMP_DIR/cases.zip" | awk '{print $1}')"

curl -fsS -X POST "$API_URL/api/v1/problems/$PROBLEM_ID/testcase-sets" \
  -H "authorization: Bearer $TOKEN" \
  -F "archive=@$TMP_DIR/cases.zip;type=application/zip" \
  -F "case_count=1" \
  -F "checksum_sha256=$SHA" >/dev/null

CHECK_RESPONSE="$(api_json POST "/api/v1/problems/$PROBLEM_ID/checks" '{}')"
CHECK_VALID="$(jq -r '.data.summary.valid' <<<"$CHECK_RESPONSE")"
if [[ "$CHECK_VALID" != "true" ]]; then
  echo "problem check did not validate current testcase set: $CHECK_RESPONSE" >&2
  exit 1
fi

api_json PATCH "/api/v1/problems/$PROBLEM_ID" '{"status":"published"}' >/dev/null

SUBMISSION_PAYLOAD="$(jq -cn --argjson problem_id "$PROBLEM_ID" --argjson language_id "$LANG_ID" --arg source "$SOURCE_CODE" '{problem_id:$problem_id,language_id:$language_id,source_code:$source}')"
SUBMISSION_RESPONSE="$(api_json POST /api/v1/submissions "$SUBMISSION_PAYLOAD")"
SUBMISSION_ID="$(jq -r '.data.id' <<<"$SUBMISSION_RESPONSE")"

STATUS=""
for _ in $(seq 1 30); do
  STATUS="$(api_json GET "/api/v1/submissions/$SUBMISSION_ID" | jq -r '.data.status')"
  [[ "$STATUS" == "accepted" ]] && break
  sleep 1
done
if [[ "$STATUS" != "accepted" ]]; then
  echo "submission $SUBMISSION_ID ended with status $STATUS" >&2
  exit 1
fi

ORIGINAL_ATTEMPT_ID="$(compose exec -T postgres psql -U soj -d soj -Atc "select max(id) from judge_attempts where submission_id = $SUBMISSION_ID;" | tr -d '\r[:space:]')"
REJUDGE_RESPONSE="$(api_json POST /api/v1/rejudge-batches "{\"problem_id\":$PROBLEM_ID,\"reason\":\"smoke rejudge\"}")"
REJUDGE_BATCH_ID="$(jq -r '.data.id' <<<"$REJUDGE_RESPONSE")"
REJUDGE_STATUS=""
for _ in $(seq 1 30); do
  REJUDGE_STATUS="$(api_json GET "/api/v1/rejudge-batches/$REJUDGE_BATCH_ID" | jq -r '.data.batch.status')"
  [[ "$REJUDGE_STATUS" == "completed" ]] && break
  sleep 1
done
if [[ "$REJUDGE_STATUS" != "completed" ]]; then
  echo "rejudge batch $REJUDGE_BATCH_ID ended with status $REJUDGE_STATUS" >&2
  exit 1
fi

REJUDGE_DB_ROWS="$(compose exec -T postgres psql -U soj -d soj -Atc "
select count(*)
from rejudge_batch_items rbi
join judge_attempts ja on ja.id = rbi.attempt_id
where rbi.batch_id = $REJUDGE_BATCH_ID
  and rbi.submission_id = $SUBMISSION_ID
  and rbi.status = 'completed'
  and ja.rejudge_batch_id = $REJUDGE_BATCH_ID
  and ja.id > $ORIGINAL_ATTEMPT_ID;
" | tr -d '\r[:space:]')"
if [[ "$REJUDGE_DB_ROWS" != "1" ]]; then
  echo "rejudge DB state rows = $REJUDGE_DB_ROWS, want 1" >&2
  exit 1
fi

compose stop worker >/dev/null
CANCEL_REJUDGE_RESPONSE="$(api_json POST /api/v1/rejudge-batches "{\"problem_id\":$PROBLEM_ID,\"reason\":\"smoke cancel\"}")"
CANCEL_REJUDGE_BATCH_ID="$(jq -r '.data.id' <<<"$CANCEL_REJUDGE_RESPONSE")"
api_json POST "/api/v1/rejudge-batches/$CANCEL_REJUDGE_BATCH_ID/cancel" '{"reason":"smoke cancel before dispatch"}' >/dev/null
CANCELED_DB_ROWS="$(compose exec -T postgres psql -U soj -d soj -Atc "
select count(*)
from rejudge_batch_items rbi
join submissions s on s.id = rbi.submission_id
join judge_tasks jt on jt.id = rbi.task_id
where rbi.batch_id = $CANCEL_REJUDGE_BATCH_ID
  and rbi.submission_id = $SUBMISSION_ID
  and rbi.status = 'canceled'
  and s.status = 'accepted'
  and jt.status = 'done';
" | tr -d '\r[:space:]')"
if [[ "$CANCELED_DB_ROWS" != "1" ]]; then
  echo "canceled rejudge DB state rows = $CANCELED_DB_ROWS, want 1" >&2
  exit 1
fi
compose start worker >/dev/null
wait_http "${WORKER_URL:-http://localhost:8081}/readyz"

if [[ "$SMOKE_REAL_JUDGE" == "1" ]]; then
  WRONG_PAYLOAD="$(jq -cn --argjson problem_id "$PROBLEM_ID" --argjson language_id "$LANG_ID" --arg source "$WRONG_SOURCE_CODE" '{problem_id:$problem_id,language_id:$language_id,source_code:$source}')"
  WRONG_RESPONSE="$(api_json POST /api/v1/submissions "$WRONG_PAYLOAD")"
  WRONG_SUBMISSION_ID="$(jq -r '.data.id' <<<"$WRONG_RESPONSE")"
  WRONG_STATUS=""
  for _ in $(seq 1 30); do
    WRONG_STATUS="$(api_json GET "/api/v1/submissions/$WRONG_SUBMISSION_ID" | jq -r '.data.status')"
    [[ "$WRONG_STATUS" == "wrong_answer" ]] && break
    sleep 1
  done
  if [[ "$WRONG_STATUS" != "wrong_answer" ]]; then
    echo "wrong-answer submission $WRONG_SUBMISSION_ID ended with status $WRONG_STATUS" >&2
    exit 1
  fi
fi

CONTEST_RESPONSE="$(api_json POST /api/v1/contests "{\"title\":\"Smoke Contest $RUN_ID\",\"visibility\":\"public\",\"status\":\"published\",\"start_at\":\"2026-01-01T00:00:00Z\",\"end_at\":\"2030-01-01T00:00:00Z\",\"freeze_at\":\"2029-01-01T00:00:00Z\",\"problems\":[{\"problem_id\":$PROBLEM_ID,\"alias\":\"A\"}]}")"
CONTEST_ID="$(jq -r '.data.id' <<<"$CONTEST_RESPONSE")"

api_json POST "/api/v1/contests/$CONTEST_ID/registrations" "{\"display_name\":\"smoke-$RUN_ID\",\"email\":\"smoke-$RUN_ID@example.com\"}" >/dev/null

CONTEST_SUBMISSION_PAYLOAD="$(jq -cn --argjson problem_id "$PROBLEM_ID" --argjson contest_id "$CONTEST_ID" --argjson language_id "$LANG_ID" --arg source "$SOURCE_CODE" '{problem_id:$problem_id,contest_id:$contest_id,language_id:$language_id,source_code:$source}')"
CONTEST_SUBMISSION_RESPONSE="$(api_json POST /api/v1/submissions "$CONTEST_SUBMISSION_PAYLOAD")"
CONTEST_SUBMISSION_ID="$(jq -r '.data.id' <<<"$CONTEST_SUBMISSION_RESPONSE")"

CONTEST_STATUS=""
for _ in $(seq 1 30); do
  CONTEST_STATUS="$(api_json GET "/api/v1/submissions/$CONTEST_SUBMISSION_ID" | jq -r '.data.status')"
  [[ "$CONTEST_STATUS" == "accepted" ]] && break
  sleep 1
done
if [[ "$CONTEST_STATUS" != "accepted" ]]; then
  echo "contest submission $CONTEST_SUBMISSION_ID ended with status $CONTEST_STATUS" >&2
  exit 1
fi
require_metric "${WORKER_URL:-http://localhost:8081}" "soj_worker_judge_task_dispatch_total"

RESULT_STREAM_LEN="0"
for _ in $(seq 1 30); do
  RESULT_STREAM_LEN="$(compose exec -T redis redis-cli XLEN "${SOJ_JUDGE_RESULT_STREAM:-soj:judge:results}" | tr -d '\r')"
  [[ "$RESULT_STREAM_LEN" != "0" ]] && break
  sleep 1
done
if [[ "$RESULT_STREAM_LEN" == "0" ]]; then
  echo "judge result stream did not receive any result events" >&2
  exit 1
fi

ASYNC_DB_ROWS="$(compose exec -T postgres psql -U soj -d soj -Atc "
select count(*)
from submissions s
join judge_tasks jt on jt.submission_id = s.id
join judge_attempts ja on ja.submission_id = s.id and ja.task_id = jt.id
join submission_results sr on sr.submission_id = s.id and sr.attempt_id = ja.id
where s.id in ($SUBMISSION_ID, $CONTEST_SUBMISSION_ID)
  and s.status = 'accepted'
  and ja.status = 'accepted'
  and sr.status = 'accepted'
  and jt.status = 'done';
" | tr -d '\r[:space:]')"
if [[ "$ASYNC_DB_ROWS" != "2" ]]; then
  echo "async judge DB state rows = $ASYNC_DB_ROWS, want 2" >&2
  compose exec -T postgres psql -U soj -d soj -c "
select s.id as submission_id, s.status as submission_status, jt.status as task_status, ja.id as attempt_id, ja.status as attempt_status, sr.status as result_status
from submissions s
left join judge_tasks jt on jt.submission_id = s.id
left join judge_attempts ja on ja.submission_id = s.id
left join submission_results sr on sr.submission_id = s.id
where s.id in ($SUBMISSION_ID, $CONTEST_SUBMISSION_ID)
order by s.id, ja.id;
" >&2
  exit 1
fi

CASE_RESULT_ROWS="$(compose exec -T postgres psql -U soj -d soj -Atc "
select count(*)
from judge_case_results jcr
join judge_attempts ja on ja.id = jcr.attempt_id
where ja.submission_id in ($SUBMISSION_ID, $CONTEST_SUBMISSION_ID);
" | tr -d '\r[:space:]')"

SCOREBOARD="$(api_json GET "/api/v1/contests/$CONTEST_ID/scoreboard?view=live")"
ACCEPTED_COUNT="$(jq -r '.data.rows[0].accepted_count' <<<"$SCOREBOARD")"
if [[ "$ACCEPTED_COUNT" != "1" ]]; then
  echo "scoreboard accepted_count = $ACCEPTED_COUNT, want 1" >&2
  exit 1
fi

api_json PATCH "/api/v1/contests/$CONTEST_ID" '{"status":"ended","end_at":"2026-01-03T00:00:00Z","freeze_at":"2026-01-02T00:00:00Z"}' >/dev/null
FINAL_SNAPSHOT_ID=""
for _ in $(seq 1 45); do
  FINAL_SNAPSHOT_ID="$(compose exec -T postgres psql -U soj -d soj -Atc "select max(id) from contest_score_snapshots where contest_id = $CONTEST_ID and kind = 'final';" | tr -d '\r[:space:]')"
  [[ -n "$FINAL_SNAPSHOT_ID" ]] && break
  sleep 1
done
if [[ -z "$FINAL_SNAPSHOT_ID" ]]; then
  echo "initial final scoreboard snapshot was not generated" >&2
  exit 1
fi

CONTEST_REJUDGE_RESPONSE="$(api_json POST /api/v1/rejudge-batches "{\"contest_id\":$CONTEST_ID,\"reason\":\"smoke contest rejudge\"}")"
CONTEST_REJUDGE_BATCH_ID="$(jq -r '.data.id' <<<"$CONTEST_REJUDGE_RESPONSE")"
CONTEST_REJUDGE_STATUS=""
for _ in $(seq 1 30); do
  CONTEST_REJUDGE_STATUS="$(api_json GET "/api/v1/rejudge-batches/$CONTEST_REJUDGE_BATCH_ID" | jq -r '.data.batch.status')"
  [[ "$CONTEST_REJUDGE_STATUS" == "completed" ]] && break
  sleep 1
done
if [[ "$CONTEST_REJUDGE_STATUS" != "completed" ]]; then
  echo "contest rejudge batch $CONTEST_REJUDGE_BATCH_ID ended with status $CONTEST_REJUDGE_STATUS" >&2
  exit 1
fi

REFRESHED_FINAL_SNAPSHOT_ID="$FINAL_SNAPSHOT_ID"
for _ in $(seq 1 45); do
  REFRESHED_FINAL_SNAPSHOT_ID="$(compose exec -T postgres psql -U soj -d soj -Atc "select max(id) from contest_score_snapshots where contest_id = $CONTEST_ID and kind = 'final';" | tr -d '\r[:space:]')"
  [[ "$REFRESHED_FINAL_SNAPSHOT_ID" -gt "$FINAL_SNAPSHOT_ID" ]] && break
  sleep 1
done
if [[ "$REFRESHED_FINAL_SNAPSHOT_ID" -le "$FINAL_SNAPSHOT_ID" ]]; then
  echo "final scoreboard snapshot was not refreshed after contest rejudge" >&2
  exit 1
fi

echo "smoke ok: problem=$PROBLEM_ID submission=$SUBMISSION_ID rejudge_batch=$REJUDGE_BATCH_ID canceled_rejudge_batch=$CANCEL_REJUDGE_BATCH_ID contest=$CONTEST_ID contest_submission=$CONTEST_SUBMISSION_ID contest_rejudge_batch=$CONTEST_REJUDGE_BATCH_ID final_snapshot=$REFRESHED_FINAL_SNAPSHOT_ID judge_results=$RESULT_STREAM_LEN async_rows=$ASYNC_DB_ROWS case_results=$CASE_RESULT_ROWS"
