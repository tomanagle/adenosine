-- name: CreateOutboxEvent :one
INSERT INTO ops.outbox_events (
    id, type, aggregate_type, aggregate_id, payload, created_at, available_at, traceparent, tracestate
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: CreateOutboxEventIfAbsent :exec
INSERT INTO ops.outbox_events (
    id, type, aggregate_type, aggregate_id, payload, created_at, available_at, traceparent, tracestate
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO NOTHING;

-- name: CreateRepositoryActivityEvent :exec
WITH target AS (
  SELECT repository.local_repository_id AS repository_id
  FROM network.repositories AS repository
  WHERE repository.uri = sqlc.arg(subject_uri)
  UNION ALL
  SELECT repository.local_repository_id
  FROM network.issues AS issue
  JOIN network.repositories AS repository ON repository.uri = issue.repository_uri
  WHERE issue.uri = sqlc.arg(subject_uri)
  UNION ALL
  SELECT repository.local_repository_id
  FROM network.pull_requests AS pull
  JOIN network.repositories AS repository ON repository.uri = pull.target_repository_uri
  WHERE pull.uri = sqlc.arg(subject_uri)
  LIMIT 1
)
INSERT INTO ops.outbox_events (
  id, type, aggregate_type, aggregate_id, payload, created_at, available_at, traceparent, tracestate
)
SELECT sqlc.arg(id), sqlc.arg(type), 'repository', target.repository_id::text,
       sqlc.arg(payload), sqlc.arg(created_at), sqlc.arg(created_at), sqlc.narg(traceparent), sqlc.narg(tracestate)
FROM target
WHERE target.repository_id IS NOT NULL;

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

-- name: RetryOutboxEvent :exec
UPDATE ops.outbox_events
SET available_at = sqlc.arg(available_at), claimed_at = NULL, claimed_by = NULL,
    last_error_code = sqlc.arg(last_error_code)
WHERE id = sqlc.arg(id) AND completed_at IS NULL;
