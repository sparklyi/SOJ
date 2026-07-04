#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/docker-compose.yaml}"
API_URL="${API_URL:-http://localhost:8080}"
RUN_ID="${RUN_ID:-$(date +%s)-$$}"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
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
  if ! curl -fsS "$url/metrics" | grep -q "$metric"; then
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
require_metric "$API_URL" "soj_http_requests_total"

LANG_ID="$(docker compose -f "$COMPOSE_FILE" exec -T postgres psql -U soj -d soj -Atc "select id from languages where engine = 'fake' and engine_language_id = 'accepted' and enabled = true order by id limit 1;")"
if [[ -z "$LANG_ID" ]]; then
  echo "fake language seed was not found" >&2
  exit 1
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

api_json PATCH "/api/v1/problems/$PROBLEM_ID" '{"status":"published"}' >/dev/null

SUBMISSION_RESPONSE="$(api_json POST /api/v1/submissions "{\"problem_id\":$PROBLEM_ID,\"language_id\":$LANG_ID,\"source_code\":\"print(2)\"}")"
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

CONTEST_RESPONSE="$(api_json POST /api/v1/contests "{\"title\":\"Smoke Contest $RUN_ID\",\"visibility\":\"public\",\"status\":\"published\",\"start_at\":\"2026-01-01T00:00:00Z\",\"end_at\":\"2030-01-01T00:00:00Z\",\"freeze_at\":\"2029-01-01T00:00:00Z\",\"problems\":[{\"problem_id\":$PROBLEM_ID,\"alias\":\"A\"}]}")"
CONTEST_ID="$(jq -r '.data.id' <<<"$CONTEST_RESPONSE")"

api_json POST "/api/v1/contests/$CONTEST_ID/registrations" "{\"display_name\":\"smoke-$RUN_ID\",\"email\":\"smoke-$RUN_ID@example.com\"}" >/dev/null

CONTEST_SUBMISSION_RESPONSE="$(api_json POST /api/v1/submissions "{\"problem_id\":$PROBLEM_ID,\"contest_id\":$CONTEST_ID,\"language_id\":$LANG_ID,\"source_code\":\"print(2)\"}")"
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
require_metric "${WORKER_URL:-http://localhost:8081}" "soj_worker_judge_tasks_total"

SCOREBOARD="$(api_json GET "/api/v1/contests/$CONTEST_ID/scoreboard?view=live")"
ACCEPTED_COUNT="$(jq -r '.data.rows[0].accepted_count' <<<"$SCOREBOARD")"
if [[ "$ACCEPTED_COUNT" != "1" ]]; then
  echo "scoreboard accepted_count = $ACCEPTED_COUNT, want 1" >&2
  exit 1
fi

echo "smoke ok: problem=$PROBLEM_ID submission=$SUBMISSION_ID contest=$CONTEST_ID contest_submission=$CONTEST_SUBMISSION_ID"
