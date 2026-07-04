# SOJ v2 Worker

The worker has two responsibilities:

- Dispatch pending `judge_tasks` rows from PostgreSQL to Redis Stream.
- Consume Redis Stream messages, call `JudgeEngine`, and write terminal results idempotently.

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

Docker Compose uses `SOJ_JUDGE_ENDPOINT=fake://accepted`. The fake engine returns accepted results and exposes one language for local sync tests. The compose seed job inserts a matching enabled language row so submissions can be created immediately after startup.

## Scoreboard Snapshots

Contest scoreboards read the latest frozen/final snapshot when one exists and fall back to synchronous aggregation when missing. Automated frozen/final snapshot generation is intentionally left as a follow-up worker responsibility.
