-- Problem review workflow.

-- name: CreateProblemReviewEvent :one
INSERT INTO problem_review_events (
    problem_id,
    actor_user_id,
    from_status,
    to_status,
    decision,
    comment
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, problem_id, actor_user_id, from_status, to_status, decision, comment, created_at;

-- name: ListProblemReviewEvents :many
SELECT id, problem_id, actor_user_id, from_status, to_status, decision, comment, created_at
FROM problem_review_events
WHERE problem_id = $1
ORDER BY created_at DESC, id DESC;

-- name: ListProblemsForReview :many
SELECT p.*
FROM problems p
WHERE p.status = 'in_review'
ORDER BY p.updated_at ASC, p.id ASC
LIMIT $1 OFFSET $2;

-- name: CountProblemsForReview :one
SELECT count(*)::bigint
FROM problems
WHERE status = 'in_review';
