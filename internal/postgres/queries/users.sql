-- Owner: WP2 Auth/User

-- name: CreateUser :one
INSERT INTO users (
    email,
    password_hash,
    username,
    avatar_url,
    bio,
    role,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetUserByID :one
SELECT *
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT *
FROM users
WHERE lower(email) = lower($1);

-- name: ListUsers :many
SELECT *
FROM users
WHERE (sqlc.narg('role')::text IS NULL OR role = sqlc.narg('role')::text)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (
      sqlc.narg('keyword')::text IS NULL
      OR email ILIKE '%' || sqlc.narg('keyword')::text || '%'
      OR username ILIKE '%' || sqlc.narg('keyword')::text || '%'
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountUsers :one
SELECT count(*)::bigint
FROM users
WHERE (sqlc.narg('role')::text IS NULL OR role = sqlc.narg('role')::text)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (
      sqlc.narg('keyword')::text IS NULL
      OR email ILIKE '%' || sqlc.narg('keyword')::text || '%'
      OR username ILIKE '%' || sqlc.narg('keyword')::text || '%'
  );

-- name: UpdateUserAdminFields :one
UPDATE users
SET username = coalesce(sqlc.narg('username'), username),
    bio = coalesce(sqlc.narg('bio'), bio),
    role = coalesce(sqlc.narg('role'), role),
    status = coalesce(sqlc.narg('status'), status),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (
    user_id,
    token_hash,
    device_id,
    user_agent,
    ip,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: GetRefreshTokenByHash :one
SELECT *
FROM refresh_tokens
WHERE token_hash = $1;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET revoked_at = now()
WHERE token_hash = $1
  AND revoked_at IS NULL;

-- name: RevokeUserDeviceRefreshTokens :exec
UPDATE refresh_tokens
SET revoked_at = now()
WHERE user_id = $1
  AND device_id = $2
  AND revoked_at IS NULL;
