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

-- name: UpdateSubmissionStatus :one
UPDATE submissions
SET status = sqlc.arg('status'),
    time_ms = sqlc.narg('time_ms'),
    memory_kb = sqlc.narg('memory_kb'),
    score = coalesce(sqlc.narg('score'), score),
    error_message = sqlc.narg('error_message'),
    judged_at = CASE
        WHEN judged_at IS NULL
          AND sqlc.arg('status')::text IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'system_error', 'canceled') THEN now()
        ELSE judged_at
    END,
    updated_at = now()
WHERE id = sqlc.arg('id')
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
SELECT *
FROM judge_tasks
WHERE status = 'pending'
  AND next_run_at <= now()
ORDER BY next_run_at, id
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: UpdateJudgeTaskDispatching :one
UPDATE judge_tasks
SET status = 'dispatching',
    updated_at = now()
WHERE id = $1
  AND status = 'pending'
RETURNING *;

-- name: MarkJudgeTaskDispatched :one
UPDATE judge_tasks
SET status = 'dispatched',
    stream_id = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkJudgeTaskDone :one
UPDATE judge_tasks
SET status = 'done',
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RetryJudgeTask :one
UPDATE judge_tasks
SET status = 'pending',
    attempts = attempts + 1,
    next_run_at = sqlc.arg('next_run_at'),
    last_error = sqlc.arg('last_error'),
    updated_at = now()
WHERE id = sqlc.arg('id')
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
          AND sqlc.arg('status')::text IN ('accepted', 'wrong_answer', 'compile_error', 'runtime_error', 'time_limit', 'memory_limit', 'system_error', 'canceled') THEN now()
        ELSE finished_at
    END,
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;
