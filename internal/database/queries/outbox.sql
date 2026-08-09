-- name: CreateOutboxEvent :one
INSERT INTO ops.outbox_events (
    id, type, aggregate_type, aggregate_id, payload, created_at, available_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: CreateOutboxEventIfAbsent :exec
INSERT INTO ops.outbox_events (
    id, type, aggregate_type, aggregate_id, payload, created_at, available_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO NOTHING;

-- name: ClaimOutboxEvents :many
UPDATE ops.outbox_events AS event
SET claimed_at = sqlc.arg(claim_time),
    claimed_by = sqlc.arg(claimed_by),
    attempts = attempts + 1
WHERE event.id IN (
    SELECT candidate.id
    FROM ops.outbox_events AS candidate
    WHERE candidate.completed_at IS NULL
      AND candidate.available_at <= sqlc.arg(claim_time)
      AND (candidate.claimed_at IS NULL OR candidate.claimed_at < sqlc.arg(stale_before))
    ORDER BY candidate.available_at, candidate.created_at
    FOR UPDATE SKIP LOCKED
    LIMIT sqlc.arg(batch_size)
)
RETURNING event.*;

-- name: CompleteOutboxEvent :exec
UPDATE ops.outbox_events
SET completed_at = $2, claimed_at = NULL, claimed_by = NULL, last_error_code = NULL
WHERE id = $1;
