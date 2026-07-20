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
SELECT *
FROM submissions
WHERE id = $1;

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
    verdict,
    score,
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
    metrics,
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
    sqlc.narg('verdict'),
    sqlc.arg('score'),
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
    sqlc.arg('metrics'),
    sqlc.narg('trace_id'),
    sqlc.narg('started_at'),
    sqlc.narg('finished_at')
)
RETURNING *;

-- name: GetJudgeAttemptByID :one
SELECT *
FROM judge_attempts
WHERE id = $1;

-- name: GetLatestJudgeAttemptBySubmissionID :one
SELECT *
FROM judge_attempts
WHERE submission_id = $1
ORDER BY attempt_no DESC, id DESC
LIMIT 1;

-- name: ListLatestJudgeAttemptsBySubmissionIDs :many
SELECT DISTINCT ON (submission_id) *
FROM judge_attempts
WHERE submission_id = ANY(sqlc.arg('submission_ids')::bigint[])
ORDER BY submission_id, attempt_no DESC, id DESC;

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
    verdict = sqlc.narg('verdict'),
    score = sqlc.arg('score'),
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
    metrics = sqlc.arg('metrics'),
    trace_id = sqlc.narg('trace_id'),
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
    output_diff_summary,
    stdout_artifact_id,
    stderr_artifact_id,
    diff_artifact_id
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
    sqlc.narg('output_diff_summary'),
    sqlc.narg('stdout_artifact_id'),
    sqlc.narg('stderr_artifact_id'),
    sqlc.narg('diff_artifact_id')
)
RETURNING *;

-- name: ListJudgeCaseResultsByAttemptID :many
SELECT *
FROM judge_case_results
WHERE attempt_id = $1
ORDER BY case_index;

-- name: UpsertSubmissionResult :one
INSERT INTO submission_results (
    submission_id,
    attempt_id,
    status,
    score,
    time_ms,
    memory_kb,
    first_failed_case_index,
    first_failed_group,
    error_class,
    safe_summary
) VALUES (
    sqlc.arg('submission_id'),
    sqlc.arg('attempt_id'),
    sqlc.arg('status'),
    sqlc.arg('score'),
    sqlc.narg('time_ms'),
    sqlc.narg('memory_kb'),
    sqlc.narg('first_failed_case_index'),
    sqlc.narg('first_failed_group'),
    sqlc.narg('error_class'),
    sqlc.arg('safe_summary')
)
ON CONFLICT (submission_id) DO UPDATE
SET attempt_id = EXCLUDED.attempt_id,
    status = EXCLUDED.status,
    score = EXCLUDED.score,
    time_ms = EXCLUDED.time_ms,
    memory_kb = EXCLUDED.memory_kb,
    first_failed_case_index = EXCLUDED.first_failed_case_index,
    first_failed_group = EXCLUDED.first_failed_group,
    error_class = EXCLUDED.error_class,
    safe_summary = EXCLUDED.safe_summary,
    updated_at = now()
RETURNING *;

-- name: GetSubmissionResultBySubmissionID :one
SELECT *
FROM submission_results
WHERE submission_id = $1;

-- name: ListSubmissionResultsBySubmissionIDs :many
SELECT *
FROM submission_results
WHERE submission_id = ANY(sqlc.arg('submission_ids')::bigint[]);

-- name: GetReadyTestcaseSetByID :one
SELECT *
FROM testcase_sets
WHERE id = $1
  AND problem_id = $2
  AND status = 'ready';

-- name: ListSubmissions :many
SELECT *
FROM submissions
WHERE (sqlc.narg('user_id')::bigint IS NULL OR user_id = sqlc.narg('user_id')::bigint)
  AND (sqlc.narg('problem_id')::bigint IS NULL OR problem_id = sqlc.narg('problem_id')::bigint)
  AND (sqlc.narg('contest_id')::bigint IS NULL OR contest_id = sqlc.narg('contest_id')::bigint)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY submitted_at DESC, id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountSubmissions :one
SELECT count(*)::bigint
FROM submissions
WHERE (sqlc.narg('user_id')::bigint IS NULL OR user_id = sqlc.narg('user_id')::bigint)
  AND (sqlc.narg('problem_id')::bigint IS NULL OR problem_id = sqlc.narg('problem_id')::bigint)
  AND (sqlc.narg('contest_id')::bigint IS NULL OR contest_id = sqlc.narg('contest_id')::bigint)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text);

-- name: ListSubmissionsByUserBefore :many
SELECT *
FROM submissions
WHERE user_id = sqlc.arg('user_id')
  AND (submitted_at, id) < (
      sqlc.arg('before_submitted_at')::timestamptz,
      sqlc.arg('before_id')::bigint
  )
ORDER BY submitted_at DESC, id DESC
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
       s.status,
       s.submitted_at,
       sr.attempt_id
FROM submissions s
LEFT JOIN submission_results sr ON sr.submission_id = s.id
WHERE s.contest_id = sqlc.arg('contest_id')
  AND s.user_id = sqlc.arg('user_id')
  AND s.problem_id = sqlc.arg('problem_id')
  AND s.status IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled')
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
  AND status IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled')
ORDER BY id
FOR UPDATE;

-- name: ListEligibleContestSubmissionsForRejudge :many
SELECT *
FROM submissions
WHERE contest_id = $1
  AND status IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled')
ORDER BY id
FOR UPDATE;

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
    time_ms = NULL,
    memory_kb = NULL,
    score = 0,
    error_message = NULL,
    judged_at = NULL,
    updated_at = now()
WHERE id = $1
  AND status IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled')
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
SET status = submission_results.status,
    time_ms = submission_results.time_ms,
    memory_kb = submission_results.memory_kb,
    score = submission_results.score,
    error_message = NULL,
    judged_at = coalesce(submissions.judged_at, submission_results.updated_at),
    updated_at = now()
FROM submission_results
WHERE submissions.id = sqlc.arg('submission_id')
  AND submission_results.submission_id = submissions.id
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
SET status = sqlc.arg('status'),
    time_ms = sqlc.narg('time_ms'),
    memory_kb = sqlc.narg('memory_kb'),
    score = coalesce(sqlc.narg('score'), score),
    error_message = sqlc.narg('error_message'),
    judged_at = CASE
        WHEN judged_at IS NULL
          AND sqlc.arg('status')::text IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled') THEN now()
        ELSE judged_at
    END,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status NOT IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled')
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
    error_message = sqlc.arg('error_message'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status NOT IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled')
RETURNING *;

-- name: MarkSubmissionSystemError :one
UPDATE submissions
SET status = 'system_error',
    error_message = sqlc.arg('error_message'),
    judged_at = CASE WHEN judged_at IS NULL THEN now() ELSE judged_at END,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status NOT IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled')
RETURNING *;

-- name: CreateJudgeTask :one
INSERT INTO judge_tasks (
    submission_id,
    status,
    next_run_at
) VALUES (
    $1, $2, $3
)
RETURNING *;

-- name: GetJudgeTaskByID :one
SELECT *
FROM judge_tasks
WHERE id = $1;

-- name: GetJudgeTaskBySubmissionID :one
SELECT *
FROM judge_tasks
WHERE submission_id = $1;

-- name: ClaimPendingJudgeTasks :many
WITH claimed AS (
    SELECT id
    FROM judge_tasks
    WHERE status = 'pending'
      AND next_run_at <= now()
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
          WHERE submissions.id = judge_tasks.submission_id
            AND submissions.status = 'system_error'
      )
    RETURNING *
), recovered_submissions AS (
    UPDATE submissions
    SET status = 'queued',
        error_message = sqlc.arg('last_error'),
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
            AND submissions.status NOT IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'output_limit', 'system_error', 'canceled')
      )
    RETURNING *
), reset_submissions AS (
    UPDATE submissions
    SET status = 'queued',
        error_message = sqlc.arg('last_error'),
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
INSERT INTO runs (
    user_id,
    problem_id,
    language_id,
    status,
    source_artifact_id,
    stdin
) VALUES (
    $1, $2, $3, $4, $5, $6
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
