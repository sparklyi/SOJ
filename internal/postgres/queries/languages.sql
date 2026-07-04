-- Owner: WP4 Submission/Language

-- Sync intentionally preserves enabled on existing rows so JudgeEngine language
-- refreshes do not re-enable languages disabled by an admin.
-- name: UpsertLanguage :one
INSERT INTO languages (
    engine,
    engine_language_id,
    name,
    version,
    compile_command,
    run_command,
    default_time_limit_ms,
    default_memory_limit_kb,
    enabled
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (engine, engine_language_id) DO UPDATE
SET name = EXCLUDED.name,
    version = EXCLUDED.version,
    compile_command = EXCLUDED.compile_command,
    run_command = EXCLUDED.run_command,
    default_time_limit_ms = EXCLUDED.default_time_limit_ms,
    default_memory_limit_kb = EXCLUDED.default_memory_limit_kb,
    updated_at = now()
RETURNING *;

-- name: GetLanguageByID :one
SELECT *
FROM languages
WHERE id = $1;

-- name: GetEnabledLanguageByID :one
SELECT *
FROM languages
WHERE id = $1
  AND enabled = true;

-- name: ListLanguages :many
SELECT *
FROM languages
WHERE (sqlc.narg('enabled')::boolean IS NULL OR enabled = sqlc.narg('enabled')::boolean)
  AND (
      sqlc.narg('engine')::text IS NULL
      OR engine = sqlc.narg('engine')::text
  )
ORDER BY enabled DESC, name ASC, id ASC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountLanguages :one
SELECT count(*)::bigint
FROM languages
WHERE (sqlc.narg('enabled')::boolean IS NULL OR enabled = sqlc.narg('enabled')::boolean)
  AND (
      sqlc.narg('engine')::text IS NULL
      OR engine = sqlc.narg('engine')::text
  );

-- name: UpdateLanguageAdminFields :one
UPDATE languages
SET enabled = coalesce(sqlc.narg('enabled'), enabled),
    default_time_limit_ms = coalesce(sqlc.narg('default_time_limit_ms'), default_time_limit_ms),
    default_memory_limit_kb = coalesce(sqlc.narg('default_memory_limit_kb'), default_memory_limit_kb),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;
