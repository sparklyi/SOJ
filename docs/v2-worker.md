# SOJ v2 Worker

The worker has two production responsibilities:

- Dispatch pending `judge_tasks` rows from PostgreSQL to Redis Stream.
- Consume judge result events from Redis Stream and write terminal results idempotently.

`soj-judge-agent` is the process that consumes judge request events, runs the JudgeCore pipeline, and publishes result events. The worker remains the only process in this path that writes business database state.

PostgreSQL is the source of truth. Redis Stream messages are delivery hints and may be duplicated. Worker updates must tolerate duplicate messages and repeated terminal writes without changing `judged_at` or `finished_at`.

## Redis Stream Defaults

- Stream: `soj:judge:tasks`
- Consumer group: `judge-workers`
- Batch size: `16`
- Block time: `5s`
- Stale claim threshold: `5m` for pending dispatch claims.
- Stale running/reset threshold: `30m` for task and run reconciliation.

## Dead Letter Policy

When a task exhausts retries, the worker first marks PostgreSQL `judge_tasks.status = dead` and `submissions.status = system_error`, then writes a dead-letter message to `soj:judge:tasks:dead`, then acknowledges the original Redis message. If dead-letter publishing fails, PostgreSQL remains the operational source of truth.

## Local Judge

Docker Compose defaults to the internal `fake://accepted` path. It returns accepted results and exposes one fake language for fast local async-flow tests. The compose seed job also inserts Go and C++17 `soj-agent` language rows for local real-code smoke.

To run the local real-code path, start Compose with:

```bash
SOJ_ENV=local \
SOJ_JUDGE_ENDPOINT=agent://local \
SOJ_JUDGE_SANDBOX_BACKEND=process \
docker compose -f deploy/docker-compose.yaml up --build -d

SMOKE_REAL_JUDGE=1 ./deploy/smoke.sh
```

The `process` backend is only for `dev`, `test`, and `local`; it is not a production sandbox.

## Metrics

The worker exposes Prometheus metrics on its health server at `GET /metrics`.

- `soj_worker_judge_task_dispatch_total`: dispatch attempts from PostgreSQL to Redis Stream, labeled by result.
- `soj_worker_judge_tasks_total`: processed Redis Stream messages, labeled by result such as `success`, `retry`, `dead`, `skipped`, and `error`.
- `soj_worker_judge_task_duration_seconds`: processing latency for a judge task message.

## Scoreboard Snapshots

Contest scoreboards read the latest frozen/final snapshot when one exists and fall back to synchronous aggregation when missing. The worker reconciliation loop generates missing frozen snapshots after `freeze_at` and missing final snapshots after `end_at`.

## Rejudge Batches

Use the rejudge API instead of mutating submission or task rows directly:

```bash
curl -sS -X POST "$SOJ_API_URL/api/v1/rejudge-batches" \
  -H "Authorization: Bearer $SOJ_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"problem_id":123,"reason":"testcase correction"}'
```

The batch transaction fixes the eligible submission set, resets only `done` or `dead` judge tasks, and queues the submissions. Worker dispatch creates a higher numbered judge attempt linked by `rejudge_batch_id`. Duplicate request/result messages reuse the same attempt and guarded item transitions prevent double-counting.

Cancellation only stops items that are still queued. If cancellation races with dispatch, the API returns `rejudge.cancel_race`; retry after the in-flight item reaches running. Running attempts may finish and their results are not rolled back.

For diagnosis, correlate `rejudge_batches.id`, item `task_id`/`attempt_id`, `judge_attempts.rejudge_batch_id`, stream event ids, and `trace_id`.
