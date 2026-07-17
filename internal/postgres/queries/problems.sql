-- Owner: WP3 Problem/Storage

-- name: CreateProblem :one
INSERT INTO problems (
    owner_user_id,
    title,
    slug,
    difficulty,
    visibility,
    status,
    time_limit_ms,
    memory_limit_kb
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: GetProblemByID :one
SELECT
    p.*,
    coalesce(ps.id, 0)::bigint AS current_statement_id,
    coalesce(ts.id, 0)::bigint AS current_testcase_set_id,
    coalesce(ts.status, '')::text AS current_testcase_status
FROM problems p
LEFT JOIN problem_statements ps ON ps.problem_id = p.id AND ps.is_current = true
LEFT JOIN testcase_sets ts ON ts.problem_id = p.id AND ts.is_current = true
WHERE p.id = $1;

-- name: GetProblemBySlug :one
SELECT
    p.*,
    coalesce(ps.id, 0)::bigint AS current_statement_id,
    coalesce(ts.id, 0)::bigint AS current_testcase_set_id,
    coalesce(ts.status, '')::text AS current_testcase_status
FROM problems p
LEFT JOIN problem_statements ps ON ps.problem_id = p.id AND ps.is_current = true
LEFT JOIN testcase_sets ts ON ts.problem_id = p.id AND ts.is_current = true
WHERE p.slug = $1;

-- name: ListProblems :many
SELECT
    p.*,
    coalesce(ps.id, 0)::bigint AS current_statement_id,
    coalesce(ts.id, 0)::bigint AS current_testcase_set_id,
    coalesce(ts.status, '')::text AS current_testcase_status
FROM problems p
LEFT JOIN problem_statements ps ON ps.problem_id = p.id AND ps.is_current = true
LEFT JOIN testcase_sets ts ON ts.problem_id = p.id AND ts.is_current = true
WHERE (sqlc.narg('difficulty')::text IS NULL OR p.difficulty = sqlc.narg('difficulty')::text)
  AND (sqlc.narg('status')::text IS NULL OR p.status = sqlc.narg('status')::text)
  AND (sqlc.narg('visibility')::text IS NULL OR p.visibility = sqlc.narg('visibility')::text)
  AND (
      sqlc.narg('tag')::text IS NULL
      OR EXISTS (
          SELECT 1
          FROM problem_tag_links ptl
          JOIN problem_tags pt ON pt.id = ptl.tag_id
          WHERE ptl.problem_id = p.id
            AND pt.slug = sqlc.narg('tag')::text
      )
  )
  AND (
      sqlc.narg('keyword')::text IS NULL
      OR p.title ILIKE '%' || sqlc.narg('keyword')::text || '%'
      OR p.slug ILIKE '%' || sqlc.narg('keyword')::text || '%'
  )
  AND (sqlc.arg('owner_user_id')::bigint = 0 OR p.owner_user_id = sqlc.arg('owner_user_id')::bigint)
  AND (
      sqlc.arg('include_all')::boolean
      OR (p.status = 'published' AND p.visibility = 'public')
      OR (sqlc.arg('viewer_user_id')::bigint > 0 AND p.owner_user_id = sqlc.arg('viewer_user_id')::bigint)
  )
ORDER BY p.created_at DESC, p.id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountProblems :one
SELECT count(*)::bigint
FROM problems
WHERE (sqlc.narg('difficulty')::text IS NULL OR difficulty = sqlc.narg('difficulty')::text)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('visibility')::text IS NULL OR visibility = sqlc.narg('visibility')::text)
  AND (
      sqlc.narg('tag')::text IS NULL
      OR EXISTS (
          SELECT 1
          FROM problem_tag_links ptl
          JOIN problem_tags pt ON pt.id = ptl.tag_id
          WHERE ptl.problem_id = problems.id
            AND pt.slug = sqlc.narg('tag')::text
      )
  )
  AND (
      sqlc.narg('keyword')::text IS NULL
      OR title ILIKE '%' || sqlc.narg('keyword')::text || '%'
      OR slug ILIKE '%' || sqlc.narg('keyword')::text || '%'
  )
  AND (sqlc.arg('owner_user_id')::bigint = 0 OR owner_user_id = sqlc.arg('owner_user_id')::bigint)
  AND (
      sqlc.arg('include_all')::boolean
      OR (status = 'published' AND visibility = 'public')
      OR (sqlc.arg('viewer_user_id')::bigint > 0 AND owner_user_id = sqlc.arg('viewer_user_id')::bigint)
  );

-- name: UpdateProblem :one
UPDATE problems
SET title = coalesce(sqlc.narg('title'), title),
    slug = coalesce(sqlc.narg('slug'), slug),
    difficulty = coalesce(sqlc.narg('difficulty'), difficulty),
    visibility = coalesce(sqlc.narg('visibility'), visibility),
    status = coalesce(sqlc.narg('status'), status),
    time_limit_ms = coalesce(sqlc.narg('time_limit_ms'), time_limit_ms),
    memory_limit_kb = coalesce(sqlc.narg('memory_limit_kb'), memory_limit_kb),
    published_at = CASE
        WHEN sqlc.narg('status')::text = 'published' AND published_at IS NULL THEN now()
        WHEN sqlc.narg('status')::text IS NULL THEN published_at
        ELSE published_at
    END,
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ArchiveProblem :one
UPDATE problems
SET status = 'archived',
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: LockProblemForUpdate :one
SELECT
    p.*,
    coalesce(ps.id, 0)::bigint AS current_statement_id,
    coalesce(ts.id, 0)::bigint AS current_testcase_set_id,
    coalesce(ts.status, '')::text AS current_testcase_status
FROM problems p
LEFT JOIN problem_statements ps ON ps.problem_id = p.id AND ps.is_current = true
LEFT JOIN testcase_sets ts ON ts.problem_id = p.id AND ts.is_current = true
WHERE p.id = $1
FOR UPDATE OF p;

-- name: CreateProblemStatement :one
INSERT INTO problem_statements (
    problem_id,
    version,
    title,
    description,
    input_description,
    output_description,
    samples,
    hint,
    source,
    is_current
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING *;

-- name: ClearCurrentProblemStatement :exec
UPDATE problem_statements
SET is_current = false
WHERE problem_id = $1
  AND is_current = true;

-- name: GetCurrentProblemStatement :one
SELECT *
FROM problem_statements
WHERE problem_id = $1
  AND is_current = true;

-- name: NextProblemStatementVersion :one
SELECT coalesce(max(version), 0)::integer + 1 AS next_version
FROM problem_statements
WHERE problem_id = $1;

-- name: CreateProblemTag :one
INSERT INTO problem_tags (name, slug)
VALUES ($1, $2)
ON CONFLICT (slug) DO UPDATE
SET name = EXCLUDED.name
RETURNING *;

-- name: LinkProblemTag :exec
INSERT INTO problem_tag_links (problem_id, tag_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ClearProblemTags :exec
DELETE FROM problem_tag_links
WHERE problem_id = $1;

-- name: ListProblemTags :many
SELECT pt.*
FROM problem_tags pt
JOIN problem_tag_links ptl ON ptl.tag_id = pt.id
WHERE ptl.problem_id = $1
ORDER BY pt.name;

-- name: CreateTestcaseSet :one
INSERT INTO testcase_sets (
    problem_id,
    version,
    storage_key,
    checksum_sha256,
    size_bytes,
    case_count,
    status,
    is_current,
    created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: ClearCurrentTestcaseSet :exec
UPDATE testcase_sets
SET is_current = false
WHERE problem_id = $1
  AND is_current = true;

-- name: GetCurrentReadyTestcaseSet :one
SELECT *
FROM testcase_sets
WHERE problem_id = $1
  AND is_current = true
  AND status = 'ready';

-- name: NextTestcaseSetVersion :one
SELECT coalesce(max(version), 0)::integer + 1 AS next_version
FROM testcase_sets
WHERE problem_id = $1;

-- name: CreateProblemCheckRun :one
INSERT INTO problem_check_runs (
    problem_id,
    statement_id,
    testcase_set_id,
    requested_by,
    status,
    summary
) VALUES (
    sqlc.arg('problem_id'),
    sqlc.narg('statement_id'),
    sqlc.narg('testcase_set_id'),
    sqlc.narg('requested_by'),
    sqlc.arg('status'),
    sqlc.arg('summary')
)
RETURNING *;

-- name: GetProblemCheckRunByID :one
SELECT *
FROM problem_check_runs
WHERE id = $1;

-- name: GetLatestCompletedProblemCheckRun :one
SELECT *
FROM problem_check_runs
WHERE problem_id = sqlc.arg('problem_id')
  AND statement_id = sqlc.arg('statement_id')
  AND testcase_set_id = sqlc.arg('testcase_set_id')
  AND status = 'completed'
ORDER BY finished_at DESC NULLS LAST, id DESC
LIMIT 1;

-- name: ListProblemCheckRunsByProblemID :many
SELECT *
FROM problem_check_runs
WHERE problem_id = $1
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CompleteProblemCheckRun :one
UPDATE problem_check_runs
SET status = 'completed',
    summary = sqlc.arg('summary'),
    error_message = NULL,
    finished_at = coalesce(sqlc.narg('finished_at'), finished_at, now()),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status IN ('queued', 'running')
RETURNING *;

-- name: FailProblemCheckRun :one
UPDATE problem_check_runs
SET status = 'failed',
    summary = sqlc.arg('summary'),
    error_message = sqlc.arg('error_message'),
    finished_at = coalesce(sqlc.narg('finished_at'), finished_at, now()),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status IN ('queued', 'running')
RETURNING *;

-- name: CreateProblemCheckFinding :one
INSERT INTO problem_check_findings (
    run_id,
    severity,
    code,
    message,
    case_index,
    testcase_key,
    details
) VALUES (
    sqlc.arg('run_id'),
    sqlc.arg('severity'),
    sqlc.arg('code'),
    sqlc.arg('message'),
    sqlc.narg('case_index'),
    sqlc.narg('testcase_key'),
    sqlc.arg('details')
)
RETURNING *;

-- name: GetProblemCheckFindingByID :one
SELECT *
FROM problem_check_findings
WHERE id = $1;

-- name: ListProblemCheckFindingsByRunID :many
SELECT *
FROM problem_check_findings
WHERE run_id = $1
ORDER BY id;

-- name: GetProblemStats :one
SELECT
    p.id AS problem_id,
    coalesce(sum(status_counts.count), 0)::bigint AS total_submissions,
    coalesce(sum(status_counts.count) FILTER (WHERE status_counts.status = 'accepted'), 0)::bigint AS accepted_submissions,
    coalesce(jsonb_object_agg(status_counts.status, status_counts.count) FILTER (WHERE status_counts.status IS NOT NULL), '{}'::jsonb) AS status_counts
FROM problems p
LEFT JOIN (
    SELECT s.status, count(*)::bigint AS count
    FROM submissions s
    WHERE s.problem_id = $1
    GROUP BY s.status
) status_counts ON true
WHERE p.id = $1
GROUP BY p.id;
