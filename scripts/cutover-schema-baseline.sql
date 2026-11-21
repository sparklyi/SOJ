-- Operator-only cutover from the old 000001..000010 chain. See docs/schema-cutover.md.
BEGIN;
SET LOCAL search_path = public, pg_catalog;

-- Match the migrator lock. Stop old writers before running this file.
SELECT pg_advisory_xact_lock(6276700125626430680);
LOCK TABLE schema_migrations IN ACCESS EXCLUSIVE MODE;

DO $guard$
BEGIN
    IF (SELECT array_agg(version ORDER BY version) FROM schema_migrations)
        IS DISTINCT FROM ARRAY[
            '000001_init', '000002_problem_check_statement', '000003_problem_review',
            '000004_contest_roles', '000005_run_problem_optional', '000006_run_tasks',
            '000007_run_retention_index', '000008_drop_testcase_status',
            '000009_admin_audit', '000010_role_permissions'
        ] THEN
        RAISE EXCEPTION 'Expected exactly the legacy SOJ migrations through 000010; refusing cutover';
    END IF;
END
$guard$;

LOCK TABLE submissions, submission_results, judge_attempts, judge_case_results,
    contest_problem_results, contest_role_audit_events, audit_events, languages
    IN ACCESS EXCLUSIVE MODE;

-- These fields were placeholders in the application. Fail rather than discard
-- data added by an out-of-band writer.
DO $guard$
BEGIN
    IF EXISTS (SELECT FROM judge_attempts WHERE metrics <> '{}'::jsonb OR score <> 0)
        OR EXISTS (SELECT FROM judge_case_results WHERE stdout_artifact_id IS NOT NULL
                   OR stderr_artifact_id IS NOT NULL OR diff_artifact_id IS NOT NULL)
        OR EXISTS (SELECT FROM submissions WHERE score <> 0)
        OR EXISTS (SELECT FROM submission_results WHERE score <> 0)
    THEN
        RAISE EXCEPTION 'Legacy placeholder fields contain data; export or reconcile them before cutting over';
    END IF;
    IF EXISTS (SELECT FROM judge_attempts WHERE verdict IS NOT NULL AND verdict <> status) THEN
        RAISE EXCEPTION 'A legacy attempt has inconsistent status and verdict; reconcile before cutting over';
    END IF;
    IF EXISTS (
        SELECT FROM submission_results r JOIN judge_attempts a ON a.id = r.attempt_id
        WHERE a.submission_id IS DISTINCT FROM r.submission_id OR a.finished_at IS NULL
            OR (a.status, a.time_ms, a.memory_kb)
                IS DISTINCT FROM (r.status, r.time_ms, r.memory_kb)
    ) THEN
        RAISE EXCEPTION 'A legacy result does not match its judge attempt; reconcile before cutting over';
    END IF;
END
$guard$;

ALTER TABLE submissions ADD COLUMN effective_attempt_id bigint;
ALTER TABLE judge_attempts ADD CONSTRAINT judge_attempts_submission_id_id_key UNIQUE (submission_id, id);
ALTER TABLE submissions ADD CONSTRAINT submissions_effective_attempt_fk
    FOREIGN KEY (id, effective_attempt_id) REFERENCES judge_attempts (submission_id, id);

UPDATE submissions s SET effective_attempt_id = r.attempt_id
FROM submission_results r WHERE r.submission_id = s.id;

-- Legacy dispatch/queue failures could mark a submission terminal without
-- publishing a result. Turn those historical outcomes into real attempts.
WITH missing AS (
    SELECT s.id, s.language_id, s.testcase_set_id, s.status, s.time_ms,
           s.memory_kb, s.error_message, COALESCE(s.judged_at, s.updated_at) AS done_at,
           (SELECT COALESCE(max(a.attempt_no), 0) + 1 FROM judge_attempts a
            WHERE a.submission_id = s.id) AS next_attempt
    FROM submissions s
    LEFT JOIN judge_attempts a ON a.id = s.effective_attempt_id
    WHERE s.status NOT IN ('queued', 'running')
      AND (a.status, a.time_ms, a.memory_kb) IS DISTINCT FROM (s.status, s.time_ms, s.memory_kb)
), recovered AS (
    INSERT INTO judge_attempts (submission_id, attempt_no, protocol_version,
        judge_core_version, judge_engine, language_id, testcase_set_id, status,
        verdict, time_ms, memory_kb, error_class, error_message, finished_at)
    SELECT id, next_attempt, 'legacy', 'legacy', 'legacy', language_id,
        testcase_set_id, status, status, time_ms, memory_kb,
        CASE WHEN status = 'system_error' THEN 'system_error' END, error_message, done_at
    FROM missing
    RETURNING id, submission_id
)
UPDATE submissions s SET effective_attempt_id = r.id FROM recovered r WHERE s.id = r.submission_id;

ALTER TABLE submissions DROP CONSTRAINT submissions_status_check;
UPDATE submissions SET status = 'judged' WHERE status NOT IN ('queued', 'running');
ALTER TABLE submissions
    ADD CONSTRAINT submissions_status_check CHECK (status IN ('queued', 'running', 'judged')),
    ADD CONSTRAINT submissions_judged_attempt_check CHECK (status <> 'judged' OR effective_attempt_id IS NOT NULL),
    DROP COLUMN time_ms,
    DROP COLUMN memory_kb,
    DROP COLUMN score,
    DROP COLUMN error_message;
DROP TABLE submission_results;

ALTER TABLE judge_attempts DROP COLUMN metrics, DROP COLUMN score, DROP COLUMN verdict;
ALTER TABLE judge_case_results DROP COLUMN stdout_artifact_id,
    DROP COLUMN stderr_artifact_id, DROP COLUMN diff_artifact_id;
ALTER TABLE contest_problem_results DROP COLUMN best_submission_id,
    DROP COLUMN best_attempt_id, DROP COLUMN last_attempt_id;
DROP INDEX contest_score_snapshot_rows_user_idx;
ALTER TABLE languages DROP COLUMN version, DROP COLUMN compile_command,
    DROP COLUMN run_command;

ALTER TABLE audit_events DROP CONSTRAINT audit_events_action_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_action_check CHECK (action IN (
    'user.role.granted', 'user.role.revoked', 'user.disabled', 'user.enabled',
    'user.deleted', 'language.enabled', 'language.disabled', 'problem.archived',
    'problem.restored', 'contest.archived', 'role.permissions.updated',
    'contest.role.granted', 'contest.role.revoked'
));

INSERT INTO audit_events (actor_user_id, action, object_type, object_id, reason, metadata, created_at)
SELECT actor_user_id,
    CASE action WHEN 'granted' THEN 'contest.role.granted' ELSE 'contest.role.revoked' END,
    'contest', contest_id, reason,
    jsonb_build_object('user_id', user_id::text, 'role', role_code), created_at
FROM contest_role_audit_events ORDER BY id;
DROP TABLE contest_role_audit_events;

INSERT INTO schema_migrations (version) VALUES ('000001_baseline');
COMMIT;
