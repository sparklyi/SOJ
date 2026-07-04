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
SELECT *
FROM problems
WHERE id = $1;

-- name: GetProblemBySlug :one
SELECT *
FROM problems
WHERE slug = $1;

-- name: ListProblems :many
SELECT *
FROM problems
WHERE (sqlc.narg('difficulty')::text IS NULL OR difficulty = sqlc.narg('difficulty')::text)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('visibility')::text IS NULL OR visibility = sqlc.narg('visibility')::text)
  AND (
      sqlc.narg('keyword')::text IS NULL
      OR title ILIKE '%' || sqlc.narg('keyword')::text || '%'
      OR slug ILIKE '%' || sqlc.narg('keyword')::text || '%'
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountProblems :one
SELECT count(*)::bigint
FROM problems
WHERE (sqlc.narg('difficulty')::text IS NULL OR difficulty = sqlc.narg('difficulty')::text)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('visibility')::text IS NULL OR visibility = sqlc.narg('visibility')::text)
  AND (
      sqlc.narg('keyword')::text IS NULL
      OR title ILIKE '%' || sqlc.narg('keyword')::text || '%'
      OR slug ILIKE '%' || sqlc.narg('keyword')::text || '%'
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
SELECT *
FROM problems
WHERE id = $1
FOR UPDATE;

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

-- name: GetProblemStats :one
SELECT
    p.id AS problem_id,
    count(s.id)::bigint AS total_submissions,
    count(s.id) FILTER (WHERE s.status = 'accepted')::bigint AS accepted_submissions,
    coalesce(jsonb_object_agg(s.status, status_counts.count) FILTER (WHERE s.status IS NOT NULL), '{}'::jsonb) AS status_counts
FROM problems p
LEFT JOIN submissions s ON s.problem_id = p.id
LEFT JOIN (
    SELECT problem_id, status, count(*)::bigint AS count
    FROM submissions
    GROUP BY problem_id, status
) status_counts ON status_counts.problem_id = s.problem_id AND status_counts.status = s.status
WHERE p.id = $1
GROUP BY p.id;
