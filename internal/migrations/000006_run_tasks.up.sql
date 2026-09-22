-- Self-runs now execute on the judge-agent, so they need a task row exactly like
-- a submission does. judge_tasks is already "a unit of work awaiting execution":
-- the same lifecycle (pending -> dispatching -> dispatched -> running ->
-- done / dead), the same attempts counter, the same next_run_at backoff and the
-- same dead-letter path. A separate run_tasks table would mean a second
-- dispatcher, a second failure handler and a second reconciler -- concurrency
-- and retry logic is the last thing that should exist twice.
--
-- The schema already anticipated this: judge_attempts.run_id exists, and
-- RequestEvent carries run_id alongside submission_id.
--
-- Exactly one subject per task. No kind column: run_id IS NULL already says
-- which one it is, and a second column would be a second source of truth.
ALTER TABLE judge_tasks ALTER COLUMN submission_id DROP NOT NULL;
ALTER TABLE judge_tasks ADD COLUMN run_id bigint REFERENCES runs(id) ON DELETE CASCADE;
ALTER TABLE judge_tasks ADD CONSTRAINT judge_tasks_subject_check
    CHECK ((submission_id IS NULL) <> (run_id IS NULL));

-- The existing UNIQUE (submission_id) index still holds: PostgreSQL does not
-- constrain NULLs, so any number of run tasks can coexist with it.
CREATE UNIQUE INDEX judge_tasks_run_id_uidx ON judge_tasks (run_id) WHERE run_id IS NOT NULL;

-- Run tasks are claimed from a different stream, so they need their own pending
-- scan. Partial, so it does not grow with submission traffic.
CREATE INDEX judge_tasks_run_pending_idx ON judge_tasks (status, next_run_at)
    WHERE run_id IS NOT NULL;
