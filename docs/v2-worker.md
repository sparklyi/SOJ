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

Contest scoreboards read the latest frozen/final snapshot when one exists and fall back to synchronous aggregation when missing. Automated frozen/final snapshot generation is intentionally left as a follow-up worker responsibility.
