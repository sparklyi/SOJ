-- name: ListAuditEvents :many
SELECT
    e.id,
    e.actor_user_id,
    u.username AS actor_username,
    e.action,
    e.object_type,
    e.object_id,
    e.reason,
    e.metadata,
    e.created_at
FROM audit_events AS e
LEFT JOIN users AS u ON u.id = e.actor_user_id
WHERE (sqlc.narg('object_type')::text IS NULL OR e.object_type = sqlc.narg('object_type')::text)
  AND (sqlc.narg('object_id')::bigint IS NULL OR e.object_id = sqlc.narg('object_id')::bigint)
  AND (sqlc.narg('actor_id')::bigint IS NULL OR e.actor_user_id = sqlc.narg('actor_id')::bigint)
  AND (sqlc.narg('action')::text IS NULL OR e.action = sqlc.narg('action')::text)
ORDER BY e.created_at DESC, e.id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountAuditEvents :one
SELECT count(*)::bigint
FROM audit_events AS e
WHERE (sqlc.narg('object_type')::text IS NULL OR e.object_type = sqlc.narg('object_type')::text)
  AND (sqlc.narg('object_id')::bigint IS NULL OR e.object_id = sqlc.narg('object_id')::bigint)
  AND (sqlc.narg('actor_id')::bigint IS NULL OR e.actor_user_id = sqlc.narg('actor_id')::bigint)
  AND (sqlc.narg('action')::text IS NULL OR e.action = sqlc.narg('action')::text);
