-- Owner: WP5 Contest

-- name: CreateContest :one
INSERT INTO contests (
    owner_user_id,
    title,
    description,
    visibility,
    status,
    start_at,
    end_at,
    freeze_at,
    invite_code_hash
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: GetContestByID :one
SELECT *
FROM contests
WHERE id = $1;

-- name: ListContests :many
SELECT *
FROM contests
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('visibility')::text IS NULL OR visibility = sqlc.narg('visibility')::text)
  AND (
      sqlc.narg('keyword')::text IS NULL
      OR title ILIKE '%' || sqlc.narg('keyword')::text || '%'
  )
ORDER BY start_at DESC, id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountContests :one
SELECT count(*)::bigint
FROM contests
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('visibility')::text IS NULL OR visibility = sqlc.narg('visibility')::text)
  AND (
      sqlc.narg('keyword')::text IS NULL
      OR title ILIKE '%' || sqlc.narg('keyword')::text || '%'
  );

-- name: UpdateContest :one
UPDATE contests
SET title = coalesce(sqlc.narg('title'), title),
    description = coalesce(sqlc.narg('description'), description),
    visibility = coalesce(sqlc.narg('visibility'), visibility),
    status = coalesce(sqlc.narg('status'), status),
    start_at = coalesce(sqlc.narg('start_at'), start_at),
    end_at = coalesce(sqlc.narg('end_at'), end_at),
    freeze_at = coalesce(sqlc.narg('freeze_at'), freeze_at),
    invite_code_hash = coalesce(sqlc.narg('invite_code_hash'), invite_code_hash),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ArchiveContest :one
UPDATE contests
SET status = 'archived',
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: AddContestProblem :one
INSERT INTO contest_problems (
    contest_id,
    problem_id,
    alias,
    sort_order
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: ListContestProblems :many
SELECT cp.*
FROM contest_problems cp
WHERE cp.contest_id = $1
ORDER BY cp.sort_order;

-- name: CreateContestRegistration :one
INSERT INTO contest_registrations (
    contest_id,
    user_id,
    display_name,
    email,
    status
) VALUES (
    $1, $2, $3, $4, 'active'
)
RETURNING *;

-- name: GetContestRegistration :one
SELECT *
FROM contest_registrations
WHERE contest_id = $1
  AND user_id = $2;

-- name: ListContestRegistrations :many
SELECT *
FROM contest_registrations
WHERE contest_id = $1
ORDER BY registered_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: UpsertContestProblemResult :one
INSERT INTO contest_problem_results (
    contest_id,
    user_id,
    problem_id,
    status,
    attempts,
    accepted_at,
    penalty_minutes,
    last_submission_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (contest_id, user_id, problem_id) DO UPDATE
SET status = EXCLUDED.status,
    attempts = EXCLUDED.attempts,
    accepted_at = EXCLUDED.accepted_at,
    penalty_minutes = EXCLUDED.penalty_minutes,
    last_submission_id = EXCLUDED.last_submission_id,
    updated_at = now()
RETURNING *;

-- name: ListContestProblemResults :many
SELECT *
FROM contest_problem_results
WHERE contest_id = $1
ORDER BY user_id, problem_id;

-- name: CreateContestScoreSnapshot :one
INSERT INTO contest_score_snapshots (
    contest_id,
    kind,
    payload
) VALUES (
    $1, $2, $3
)
RETURNING *;

-- name: GetLatestContestScoreSnapshot :one
SELECT *
FROM contest_score_snapshots
WHERE contest_id = $1
  AND kind = $2
ORDER BY generated_at DESC, id DESC
LIMIT 1;
