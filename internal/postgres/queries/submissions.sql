-- Owner: WP4 Submission/Run

-- name: CreateArtifact :one
INSERT INTO artifacts (
    owner_type,
    owner_id,
    kind,
    storage_key,
    checksum_sha256,
    size_bytes,
    content_type
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetArtifactByID :one
SELECT *
FROM artifacts
WHERE id = $1;

-- name: CreateSubmission :one
INSERT INTO submissions (
    user_id,
    problem_id,
    contest_id,
    language_id,
    testcase_set_id,
    status,
    source_artifact_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetSubmissionByID :one
SELECT sqlc.embed(s), (CASE WHEN s.status = 'judged' THEN a.status ELSE s.status END)::text AS outcome_status
FROM submissions s
LEFT JOIN judge_attempts a ON a.id = s.effective_attempt_id
WHERE s.id = $1;

-- name: LockSubmissionByID :one
SELECT *
FROM submissions
WHERE id = $1
FOR UPDATE;

-- name: CreateJudgeAttempt :one
INSERT INTO judge_attempts (
    submission_id,
    run_id,
    task_id,
    rejudge_batch_id,
    attempt_no,
    protocol_version,
    judge_core_version,
    judge_engine,
    judge_agent_id,
    language_id,
    language_runtime,
    sandbox_backend,
    sandbox_profile,
    testcase_set_id,
    testcase_set_hash,
    checker_hash,
    validator_hash,
    status,
    time_ms,
    memory_kb,
    first_failed_case_index,
    first_failed_group,
    compile_output_summary,
    stderr_summary,
    checker_message,
    error_class,
    error_message,
    manifest,
    trace_id,
    started_at,
    finished_at
) VALUES (
    sqlc.narg('submission_id'),
    sqlc.narg('run_id'),
    sqlc.narg('task_id'),
    sqlc.narg('rejudge_batch_id'),
    sqlc.arg('attempt_no'),
    sqlc.arg('protocol_version'),
    sqlc.arg('judge_core_version'),
    sqlc.arg('judge_engine'),
    sqlc.narg('judge_agent_id'),
    sqlc.arg('language_id'),
    sqlc.narg('language_runtime'),
    sqlc.narg('sandbox_backend'),
    sqlc.narg('sandbox_profile'),
    sqlc.narg('testcase_set_id'),
    sqlc.narg('testcase_set_hash'),
    sqlc.narg('checker_hash'),
    sqlc.narg('validator_hash'),
    sqlc.arg('status'),
    sqlc.narg('time_ms'),
    sqlc.narg('memory_kb'),
    sqlc.narg('first_failed_case_index'),
    sqlc.narg('first_failed_group'),
    sqlc.narg('compile_output_summary'),
    sqlc.narg('stderr_summary'),
    sqlc.narg('checker_message'),
    sqlc.narg('error_class'),
    sqlc.narg('error_message'),
    sqlc.arg('manifest'),
    sqlc.narg('trace_id'),
    sqlc.narg('started_at'),
    sqlc.narg('finished_at')
)
RETURNING *;

-- name: GetJudgeAttemptByID :one
SELECT *
FROM judge_attempts
WHERE id = $1;

-- name: LockJudgeAttemptByID :one
SELECT *
FROM judge_attempts
WHERE id = $1
FOR UPDATE;

-- name: GetLatestJudgeAttemptBySubmissionID :one
SELECT *
FROM judge_attempts
WHERE submission_id = $1
ORDER BY attempt_no DESC, id DESC
LIMIT 1;

-- name: ListSubmissionOutcomesByAttemptIDs :many
-- Callers pass the effective attempt IDs from the submission records they read.
SELECT sqlc.embed(a), c.output_diff_summary AS first_failed_case_diff
FROM judge_attempts a
LEFT JOIN judge_case_results c
       ON c.attempt_id = a.id AND c.case_index = a.first_failed_case_index
WHERE a.id = ANY(sqlc.arg('attempt_ids')::bigint[]);

-- name: GetLatestJudgeAttemptByRunID :one
SELECT *
FROM judge_attempts
WHERE run_id = $1
ORDER BY attempt_no DESC, id DESC
LIMIT 1;

-- name: ListJudgeAttemptsBySubmissionID :many
SELECT *
FROM judge_attempts
WHERE submission_id = $1
ORDER BY attempt_no DESC, id DESC;

-- name: ListJudgeAttemptsByRejudgeBatch :many
SELECT *
FROM judge_attempts
WHERE rejudge_batch_id = $1
ORDER BY id;

-- name: MarkJudgeAttemptFinished :one
UPDATE judge_attempts
SET status = sqlc.arg('status'),
    judge_core_version = coalesce(sqlc.narg('judge_core_version'), judge_core_version),
    judge_agent_id = coalesce(sqlc.narg('judge_agent_id'), judge_agent_id),
    language_runtime = coalesce(sqlc.narg('language_runtime'), language_runtime),
    sandbox_backend = coalesce(sqlc.narg('sandbox_backend'), sandbox_backend),
    sandbox_profile = coalesce(sqlc.narg('sandbox_profile'), sandbox_profile),
    testcase_set_hash = coalesce(sqlc.narg('testcase_set_hash'), testcase_set_hash),
    checker_hash = coalesce(sqlc.narg('checker_hash'), checker_hash),
    validator_hash = coalesce(sqlc.narg('validator_hash'), validator_hash),
    time_ms = sqlc.narg('time_ms'),
    memory_kb = sqlc.narg('memory_kb'),
    first_failed_case_index = sqlc.narg('first_failed_case_index'),
    first_failed_group = sqlc.narg('first_failed_group'),
    compile_output_summary = sqlc.narg('compile_output_summary'),
    stderr_summary = sqlc.narg('stderr_summary'),
    checker_message = sqlc.narg('checker_message'),
    error_class = sqlc.narg('error_class'),
    error_message = sqlc.narg('error_message'),
    manifest = sqlc.arg('manifest'),
    trace_id = coalesce(sqlc.narg('trace_id'), trace_id),
    finished_at = coalesce(sqlc.narg('finished_at'), finished_at, now()),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: CreateJudgeCaseResult :one
INSERT INTO judge_case_results (
    attempt_id,
    case_index,
    group_name,
    testcase_key,
    status,
    score,
    time_ms,
    memory_kb,
    exit_code,
    signal,
    checker_message,
    output_diff_summary
) VALUES (
    sqlc.arg('attempt_id'),
    sqlc.arg('case_index'),
    sqlc.narg('group_name'),
    sqlc.narg('testcase_key'),
    sqlc.arg('status'),
    sqlc.arg('score'),
    sqlc.narg('time_ms'),
    sqlc.narg('memory_kb'),
    sqlc.narg('exit_code'),
    sqlc.narg('signal'),
    sqlc.narg('checker_message'),
    sqlc.narg('output_diff_summary')
)
RETURNING *;

-- name: ListJudgeCaseResultsByAttemptID :many
SELECT *
FROM judge_case_results
WHERE attempt_id = $1
ORDER BY case_index;



-- name: GetReadyTestcaseSetByID :one
SELECT *
FROM testcase_sets
WHERE id = $1
  AND problem_id = $2;

-- name: ListSubmissions :many
SELECT sqlc.embed(s), (CASE WHEN s.status = 'judged' THEN a.status ELSE s.status END)::text AS outcome_status
FROM submissions s
LEFT JOIN judge_attempts a ON a.id = s.effective_attempt_id
WHERE (sqlc.narg('user_id')::bigint IS NULL OR s.user_id = sqlc.narg('user_id')::bigint)
  AND (sqlc.narg('problem_id')::bigint IS NULL OR s.problem_id = sqlc.narg('problem_id')::bigint)
  AND (sqlc.narg('contest_id')::bigint IS NULL OR s.contest_id = sqlc.narg('contest_id')::bigint)
  AND (sqlc.narg('status')::text IS NULL OR (CASE WHEN s.status = 'judged' THEN a.status ELSE s.status END) = sqlc.narg('status')::text)
ORDER BY s.submitted_at DESC, s.id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountSubmissions :one
SELECT count(*)::bigint
FROM submissions s
LEFT JOIN judge_attempts a ON a.id = s.effective_attempt_id
WHERE (sqlc.narg('user_id')::bigint IS NULL OR s.user_id = sqlc.narg('user_id')::bigint)
  AND (sqlc.narg('problem_id')::bigint IS NULL OR s.problem_id = sqlc.narg('problem_id')::bigint)
  AND (sqlc.narg('contest_id')::bigint IS NULL OR s.contest_id = sqlc.narg('contest_id')::bigint)
  AND (sqlc.narg('status')::text IS NULL OR (CASE WHEN s.status = 'judged' THEN a.status ELSE s.status END) = sqlc.narg('status')::text);

-- name: ListSubmissionsByUserBefore :many
SELECT sqlc.embed(s), (CASE WHEN s.status = 'judged' THEN a.status ELSE s.status END)::text AS outcome_status
FROM submissions s
LEFT JOIN judge_attempts a ON a.id = s.effective_attempt_id
WHERE s.user_id = sqlc.arg('user_id')
  AND (s.submitted_at, s.id) < (
      sqlc.arg('before_submitted_at')::timestamptz,
      sqlc.arg('before_id')::bigint
  )
ORDER BY s.submitted_at DESC, s.id DESC
LIMIT sqlc.arg('limit');

-- name: EnsureContestProblemResultProjection :exec
INSERT INTO contest_problem_results (
    contest_id,
    user_id,
    problem_id,
    status,
    attempts,
    penalty_minutes
) VALUES (
    sqlc.arg('contest_id'),
    sqlc.arg('user_id'),
    sqlc.arg('problem_id'),
    'none',
    0,
    0
)
ON CONFLICT (contest_id, user_id, problem_id) DO NOTHING;

-- name: LockContestProblemResultProjection :one
SELECT *
FROM contest_problem_results
WHERE contest_id = sqlc.arg('contest_id')
  AND user_id = sqlc.arg('user_id')
  AND problem_id = sqlc.arg('problem_id')
FOR UPDATE;

-- name: ListContestProblemSubmissionsForProjection :many
SELECT s.id,
       a.status,
       s.submitted_at
FROM submissions s
JOIN judge_attempts a ON a.id = s.effective_attempt_id
WHERE s.contest_id = sqlc.arg('contest_id')
  AND s.user_id = sqlc.arg('user_id')
  AND s.problem_id = sqlc.arg('problem_id')
  AND s.status = 'judged'
ORDER BY s.submitted_at, s.id;

-- name: CreateRejudgeBatch :one
INSERT INTO rejudge_batches (
    problem_id,
    contest_id,
    requested_by,
    status,
    reason,
    filters,
    total_count
) VALUES (
    sqlc.narg('problem_id'),
    sqlc.narg('contest_id'),
    sqlc.arg('requested_by'),
    sqlc.arg('status'),
    sqlc.arg('reason'),
    sqlc.arg('filters'),
    sqlc.arg('total_count')
)
RETURNING *;

-- name: GetRejudgeBatchByID :one
SELECT *
FROM rejudge_batches
WHERE id = $1;

-- name: ListRejudgeBatches :many
SELECT *
FROM rejudge_batches
WHERE (sqlc.narg('problem_id')::bigint IS NULL OR problem_id = sqlc.narg('problem_id')::bigint)
  AND (sqlc.narg('contest_id')::bigint IS NULL OR contest_id = sqlc.narg('contest_id')::bigint)
  AND (sqlc.narg('requested_by')::bigint IS NULL OR requested_by = sqlc.narg('requested_by')::bigint)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountRejudgeBatches :one
SELECT count(*)::bigint
FROM rejudge_batches
WHERE (sqlc.narg('problem_id')::bigint IS NULL OR problem_id = sqlc.narg('problem_id')::bigint)
  AND (sqlc.narg('contest_id')::bigint IS NULL OR contest_id = sqlc.narg('contest_id')::bigint)
  AND (sqlc.narg('requested_by')::bigint IS NULL OR requested_by = sqlc.narg('requested_by')::bigint)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text);

-- name: UpdateRejudgeBatchProgress :one
UPDATE rejudge_batches
SET status = CASE
        WHEN status = 'queued' THEN 'running'
        ELSE status
    END,
    completed_count = sqlc.arg('completed_count'),
    failed_count = sqlc.arg('failed_count'),
    canceled_count = sqlc.arg('canceled_count'),
    started_at = coalesce(started_at, now()),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status IN ('queued', 'running')
RETURNING *;

-- name: CompleteRejudgeBatch :one
UPDATE rejudge_batches
SET status = 'completed',
    completed_count = sqlc.arg('completed_count'),
    failed_count = sqlc.arg('failed_count'),
    canceled_count = sqlc.arg('canceled_count'),
    error_message = NULL,
    started_at = coalesce(started_at, now()),
    finished_at = coalesce(sqlc.narg('finished_at'), finished_at, now()),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status IN ('queued', 'running')
RETURNING *;

-- name: FailRejudgeBatch :one
UPDATE rejudge_batches
SET status = 'failed',
    completed_count = sqlc.arg('completed_count'),
    failed_count = sqlc.arg('failed_count'),
    canceled_count = sqlc.arg('canceled_count'),
    error_message = sqlc.arg('error_message'),
    started_at = coalesce(started_at, now()),
    finished_at = coalesce(sqlc.narg('finished_at'), finished_at, now()),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status IN ('queued', 'running')
RETURNING *;

-- name: CancelRejudgeBatch :one
UPDATE rejudge_batches
SET status = 'canceled',
    canceled_count = sqlc.arg('canceled_count'),
    error_message = sqlc.narg('error_message'),
    finished_at = coalesce(sqlc.narg('finished_at'), finished_at, now()),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status IN ('queued', 'running')
RETURNING *;

-- name: ListEligibleProblemSubmissionsForRejudge :many
SELECT *
FROM submissions
WHERE problem_id = $1
  AND contest_id IS NULL
  AND status = 'judged'
ORDER BY id;

-- name: ListEligibleContestSubmissionsForRejudge :many
SELECT *
FROM submissions
WHERE contest_id = $1
  AND status = 'judged'
ORDER BY id;

-- name: CreateRejudgeBatchItem :one
INSERT INTO rejudge_batch_items (
    batch_id,
    submission_id,
    task_id,
    status
) VALUES (
    $1, $2, $3, 'queued'
)
RETURNING *;

-- name: ListRejudgeBatchItems :many
SELECT *
FROM rejudge_batch_items
WHERE batch_id = $1
ORDER BY id;

-- name: GetQueuedRejudgeBatchItemByTaskID :one
SELECT *
FROM rejudge_batch_items
WHERE task_id = $1
  AND status = 'queued';

-- name: PrepareJudgeTaskForRejudge :one
UPDATE judge_tasks
SET status = 'pending',
    stream_id = NULL,
    attempts = 0,
    next_run_at = sqlc.arg('next_run_at'),
    last_error = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND submission_id = sqlc.arg('submission_id')
  AND status IN ('done', 'dead')
RETURNING *;

-- name: PrepareSubmissionForRejudge :one
UPDATE submissions
SET status = 'queued',
    judged_at = NULL,
    updated_at = now()
WHERE id = $1
  AND status = 'judged'
RETURNING *;

-- name: StartRejudgeBatchItem :one
UPDATE rejudge_batch_items
SET status = 'running',
    attempt_id = sqlc.arg('attempt_id'),
    started_at = coalesce(started_at, now()),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status = 'queued'
  AND attempt_id IS NULL
RETURNING *;

-- name: FinishRejudgeBatchItem :one
UPDATE rejudge_batch_items
SET status = sqlc.arg('status'),
    error_message = sqlc.narg('error_message'),
    finished_at = coalesce(finished_at, now()),
    updated_at = now()
WHERE attempt_id = sqlc.arg('attempt_id')
  AND status = 'running'
RETURNING *;

-- name: FailActiveRejudgeBatchItemByTaskID :one
UPDATE rejudge_batch_items
SET status = 'failed',
    error_message = sqlc.arg('error_message'),
    finished_at = coalesce(finished_at, now()),
    updated_at = now()
WHERE task_id = sqlc.arg('task_id')
  AND status IN ('queued', 'running')
RETURNING *;

-- name: CancelQueuedRejudgeBatchItems :many
UPDATE rejudge_batch_items
SET status = 'canceled',
    error_message = sqlc.narg('error_message'),
    finished_at = coalesce(finished_at, now()),
    updated_at = now()
WHERE batch_id = sqlc.arg('batch_id')
  AND status = 'queued'
RETURNING *;

-- name: CancelPendingJudgeTaskForRejudge :one
UPDATE judge_tasks
SET status = 'done',
    stream_id = NULL,
    last_error = sqlc.narg('last_error'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status = 'pending'
RETURNING *;

-- name: RestoreSubmissionAfterCanceledRejudge :one
UPDATE submissions
SET status = 'judged',
    judged_at = judge_attempts.finished_at,
    updated_at = now()
FROM judge_attempts
WHERE submissions.id = sqlc.arg('submission_id')
  AND judge_attempts.id = submissions.effective_attempt_id
  AND judge_attempts.submission_id = submissions.id
  AND submissions.status = 'queued'
RETURNING submissions.*;

-- name: RefreshRejudgeBatchProgress :one
WITH counts AS (
    SELECT count(*) FILTER (WHERE status = 'completed')::integer AS completed_count,
           count(*) FILTER (WHERE status = 'failed')::integer AS failed_count,
           count(*) FILTER (WHERE status = 'canceled')::integer AS canceled_count,
           count(*) FILTER (WHERE status IN ('queued', 'running'))::integer AS active_count
    FROM rejudge_batch_items
    WHERE batch_id = $1
)
UPDATE rejudge_batches
SET status = CASE
        WHEN rejudge_batches.status = 'canceled' THEN 'canceled'
        WHEN counts.active_count = 0 AND counts.failed_count > 0 THEN 'failed'
        WHEN counts.active_count = 0 THEN 'completed'
        ELSE 'running'
    END,
    completed_count = counts.completed_count,
    failed_count = counts.failed_count,
    canceled_count = counts.canceled_count,
    started_at = coalesce(started_at, now()),
    finished_at = CASE
        WHEN counts.active_count = 0 THEN coalesce(finished_at, now())
        ELSE finished_at
    END,
    updated_at = now()
FROM counts
WHERE rejudge_batches.id = $1
  AND rejudge_batches.status IN ('queued', 'running', 'canceled')
RETURNING rejudge_batches.*;

-- name: UpdateSubmissionStatus :one
UPDATE submissions
SET status = 'judged',
    effective_attempt_id = sqlc.arg('effective_attempt_id'),
    judged_at = coalesce(sqlc.narg('judged_at'), now()),
    first_judged_at = coalesce(first_judged_at, sqlc.narg('judged_at'), now()),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status IN ('queued', 'running')
RETURNING *;

-- name: MarkSubmissionRunning :one
UPDATE submissions
SET status = 'running',
    updated_at = now()
WHERE id = $1
  AND status = 'queued'
RETURNING *;

-- name: MarkSubmissionQueued :one
UPDATE submissions
SET status = 'queued',
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status IN ('queued', 'running')
RETURNING *;

-- name: CreateJudgeTask :one
-- Exactly one subject per task; the table CHECK enforces it. A run task and a
-- submission task share the whole lifecycle, which is why they share the table.
INSERT INTO judge_tasks (
    submission_id,
    run_id,
    status,
    next_run_at
) VALUES (
    sqlc.narg('submission_id'),
    sqlc.narg('run_id'),
    $1, $2
)
RETURNING *;

-- name: GetJudgeTaskByID :one
SELECT *
FROM judge_tasks
WHERE id = $1;

-- name: LockJudgeTaskByID :one
SELECT *
FROM judge_tasks
WHERE id = $1
FOR UPDATE;

-- name: GetJudgeTaskBySubmissionID :one
SELECT *
FROM judge_tasks
WHERE submission_id = $1;

-- name: ListJudgeTasksBySubmissionIDs :many
SELECT *
FROM judge_tasks
WHERE submission_id = ANY(sqlc.arg('submission_ids')::bigint[]);

-- name: ClaimPendingJudgeTasks :many
-- Submission tasks only. Runs are claimed by ClaimPendingRunTasks so they can
-- be published to their own stream: playground traffic must not sit in front of
-- formal submissions.
WITH claimed AS (
    SELECT id
    FROM judge_tasks
    WHERE status = 'pending'
      AND next_run_at <= now()
      AND submission_id IS NOT NULL
    ORDER BY next_run_at, id
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE judge_tasks
SET status = 'dispatching',
    updated_at = now()
FROM claimed
WHERE judge_tasks.id = claimed.id
  AND judge_tasks.status = 'pending'
RETURNING judge_tasks.*;

-- name: ClaimPendingRunTasks :many
WITH claimed AS (
    SELECT id
    FROM judge_tasks
    WHERE status = 'pending'
      AND next_run_at <= now()
      AND run_id IS NOT NULL
    ORDER BY next_run_at, id
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE judge_tasks
SET status = 'dispatching',
    updated_at = now()
FROM claimed
WHERE judge_tasks.id = claimed.id
  AND judge_tasks.status = 'pending'
RETURNING judge_tasks.*;

-- name: GetJudgeTaskByRunID :one
SELECT *
FROM judge_tasks
WHERE run_id = $1;

-- name: UpdateJudgeTaskDispatching :one
UPDATE judge_tasks
SET status = 'dispatching',
    updated_at = now()
WHERE id = $1
  AND status = 'pending'
RETURNING *;

-- name: MarkJudgeTaskDispatched :one
UPDATE judge_tasks
SET status = CASE
        WHEN status = 'dispatching' THEN 'dispatched'
        ELSE status
    END,
    stream_id = $2,
    updated_at = now()
WHERE id = $1
  AND status IN ('dispatching', 'running', 'done')
RETURNING *;

-- name: MarkJudgeTaskDone :one
UPDATE judge_tasks
SET status = 'done',
    updated_at = now()
WHERE id = $1
  AND status IN ('dispatching', 'dispatched', 'running')
RETURNING *;

-- name: MarkJudgeTaskRunning :one
UPDATE judge_tasks
SET status = 'running',
    updated_at = now()
WHERE id = $1
  AND status IN ('dispatching', 'dispatched', 'running')
RETURNING *;

-- name: MarkJudgeTaskDead :one
UPDATE judge_tasks
SET status = 'dead',
    last_error = $2,
    updated_at = now()
WHERE id = $1
  AND status IN ('dispatching', 'dispatched', 'running')
RETURNING *;

-- name: RecoverDeadJudgeTask :one
WITH recovered AS (
    UPDATE judge_tasks
    SET status = 'pending',
        attempts = 0,
        next_run_at = sqlc.arg('next_run_at'),
        last_error = sqlc.arg('last_error'),
        updated_at = now()
    WHERE judge_tasks.id = sqlc.arg('id')
      AND judge_tasks.status = 'dead'
      AND EXISTS (
          SELECT 1
          FROM submissions
          JOIN judge_attempts ON judge_attempts.id = submissions.effective_attempt_id
          WHERE submissions.id = judge_tasks.submission_id
            AND submissions.status = 'judged'
            AND judge_attempts.status = 'system_error'
      )
    RETURNING *
), recovered_submissions AS (
    UPDATE submissions
    SET status = 'queued',
        judged_at = NULL,
        updated_at = now()
    FROM recovered
    WHERE submissions.id = recovered.submission_id
    RETURNING submissions.id
)
SELECT *
FROM recovered;

-- name: RetryJudgeTask :one
UPDATE judge_tasks
SET status = 'pending',
    attempts = attempts + 1,
    next_run_at = sqlc.arg('next_run_at'),
    last_error = sqlc.arg('last_error'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status IN ('dispatching', 'dispatched', 'running')
RETURNING *;

-- name: ResetStaleJudgeTasks :many
WITH reset_tasks AS (
    UPDATE judge_tasks
    SET status = 'pending',
        next_run_at = now(),
        last_error = sqlc.arg('last_error'),
        updated_at = now()
    WHERE judge_tasks.status IN ('dispatching', 'running')
      AND judge_tasks.updated_at < sqlc.arg('stale_before')
      AND EXISTS (
          SELECT 1
          FROM submissions
          WHERE submissions.id = judge_tasks.submission_id
            AND submissions.status IN ('queued', 'running')
      )
    RETURNING *
), reset_submissions AS (
    UPDATE submissions
    SET status = 'queued',
        updated_at = now()
    FROM reset_tasks
    WHERE submissions.id = reset_tasks.submission_id
      AND submissions.status = 'running'
    RETURNING submissions.id
)
SELECT *
FROM reset_tasks
ORDER BY id;

-- name: MarkStaleRunsSystemError :many
UPDATE runs
SET status = 'system_error',
    error_message = sqlc.arg('error_message'),
    finished_at = CASE WHEN finished_at IS NULL THEN now() ELSE finished_at END,
    updated_at = now()
WHERE status IN ('queued', 'running')
  AND updated_at < sqlc.arg('stale_before')
RETURNING *;

-- name: CreateRun :one
-- problem_id is optional: a run attached to a problem validates the problem
-- first, a playground run passes nothing and skips that check entirely.
INSERT INTO runs (
    user_id,
    problem_id,
    language_id,
    status,
    source_artifact_id,
    stdin
) VALUES (
    sqlc.arg('user_id'),
    sqlc.narg('problem_id'),
    sqlc.arg('language_id'),
    sqlc.arg('status'),
    sqlc.arg('source_artifact_id'),
    sqlc.arg('stdin')
)
RETURNING *;

-- name: GetRunByID :one
SELECT *
FROM runs
WHERE id = $1;

-- name: UpdateRunStatus :one
UPDATE runs
SET status = sqlc.arg('status'),
    stdout = sqlc.narg('stdout'),
    stderr = sqlc.narg('stderr'),
    compile_output = sqlc.narg('compile_output'),
    time_ms = sqlc.narg('time_ms'),
    memory_kb = sqlc.narg('memory_kb'),
    error_message = sqlc.narg('error_message'),
    finished_at = CASE
        WHEN finished_at IS NULL
          AND sqlc.arg('status')::text IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled') THEN now()
        ELSE finished_at
    END,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status NOT IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled')
RETURNING *;

-- name: MarkRunRunning :one
UPDATE runs
SET status = 'running',
    updated_at = now()
WHERE id = $1
  AND status = 'queued'
RETURNING *;

-- name: LockRunByID :one
SELECT *
FROM runs
WHERE id = $1
FOR UPDATE;

-- name: CountActiveRunsByUser :one
-- "Active" is what the per-user cap is about: a run the user is still waiting
-- on. Counted in the database so the cap holds across API replicas, where an
-- in-process counter would silently be N times the configured value.
SELECT count(*)::bigint
FROM runs
WHERE user_id = $1
  AND status IN ('queued', 'running');

-- name: LockUserForRunAdmission :one
-- Serialises run admission per user so the count above cannot race an insert.
SELECT id
FROM users
WHERE id = $1
FOR UPDATE;

-- name: ListExpiredRuns :many
-- Self-runs past the retention window, with the storage key of the object that
-- has to be removed before the row.
--
-- Terminal status only. A run that never finished is MarkStaleRunsSystemError's
-- business; deleting something that might still be executing would be
-- indefensible, and that sweep has already terminated anything older than half
-- an hour anyway.
--
-- LEFT JOIN so a run whose artifact is already gone is still deleted. An inner
-- join would hide exactly the rows that most need removing.
SELECT
    runs.id,
    runs.source_artifact_id,
    artifacts.storage_key
FROM runs
LEFT JOIN artifacts ON artifacts.id = runs.source_artifact_id
WHERE runs.status NOT IN ('queued', 'running')
  AND runs.created_at < sqlc.arg('created_before')
ORDER BY runs.created_at, runs.id
LIMIT sqlc.arg('limit');

-- name: DeleteRunByID :exec
DELETE FROM runs
WHERE id = $1;

-- name: DeleteArtifactByID :exec
DELETE FROM artifacts
WHERE id = $1;

-- name: ListOrphanedRunArtifacts :many
-- Run source objects that no run points at.
--
-- These are left by a run whose source was uploaded and whose admission was then
-- refused, or whose process died in between: the object is written before the
-- decision is made, so the decision is the only place that can undo it, and a
-- crash has no place at all. Without this sweep such an object is invisible to
-- every other query in the system and stays forever.
--
-- Scoped to owner_type = 'run' and kind = 'source' so a submission's source,
-- which is kept for rejudge, can never be picked up.
SELECT artifacts.id, artifacts.storage_key
FROM artifacts
LEFT JOIN runs ON runs.source_artifact_id = artifacts.id
WHERE artifacts.owner_type = 'run'
  AND artifacts.kind = 'source'
  AND artifacts.created_at < sqlc.arg('created_before')
  AND runs.id IS NULL
ORDER BY artifacts.created_at, artifacts.id
LIMIT sqlc.arg('limit');
