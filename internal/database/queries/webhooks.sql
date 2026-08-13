-- name: CreateRepositoryWebhook :one
INSERT INTO core.repository_webhooks (
  id, repository_id, url, secret_ciphertext, events, enabled, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
RETURNING *;

-- name: GetRepositoryWebhook :one
SELECT * FROM core.repository_webhooks
WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id) AND deleted_at IS NULL;

-- name: PageRepositoryWebhooks :many
SELECT * FROM core.repository_webhooks AS webhook
WHERE webhook.repository_id = sqlc.arg(repository_id)
  AND webhook.deleted_at IS NULL
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (webhook.created_at, webhook.id) < (
      SELECT cursor.created_at, cursor.id FROM core.repository_webhooks AS cursor
      WHERE cursor.id = sqlc.narg(after_id)::uuid AND cursor.repository_id = sqlc.arg(repository_id)
    )
  )
ORDER BY webhook.created_at DESC, webhook.id DESC
LIMIT sqlc.arg(page_limit);

-- name: UpdateRepositoryWebhook :one
UPDATE core.repository_webhooks
SET url = sqlc.arg(url),
    secret_ciphertext = CASE WHEN sqlc.arg(replace_secret)::boolean THEN sqlc.arg(secret_ciphertext) ELSE secret_ciphertext END,
    events = sqlc.arg(events), enabled = sqlc.arg(enabled), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id) AND deleted_at IS NULL
RETURNING *;

-- name: DeleteRepositoryWebhook :execrows
UPDATE core.repository_webhooks SET deleted_at = sqlc.arg(deleted_at), updated_at = sqlc.arg(deleted_at)
WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id) AND deleted_at IS NULL;

-- name: CreateWebhookDeliveriesForEvent :exec
INSERT INTO ops.webhook_deliveries (
  id, webhook_id, event_type, event_id, request_body, available_at, created_at
)
SELECT md5(webhook.id::text || ':' || sqlc.arg(event_id)::uuid::text)::uuid,
       webhook.id, sqlc.arg(event_type), sqlc.arg(event_id), sqlc.arg(request_body),
       sqlc.arg(created_at), sqlc.arg(created_at)
FROM core.repository_webhooks AS webhook
WHERE webhook.repository_id = sqlc.arg(repository_id)
  AND webhook.enabled AND webhook.deleted_at IS NULL
  AND sqlc.arg(event_type) = ANY(webhook.events)
ON CONFLICT (webhook_id, event_id) DO NOTHING;

-- name: ClaimWebhookDeliveries :many
UPDATE ops.webhook_deliveries AS delivery
SET claimed_at = sqlc.arg(claim_time), claimed_by = sqlc.arg(claimed_by), attempts = attempts + 1
FROM core.repository_webhooks AS webhook
WHERE delivery.id IN (
  SELECT candidate.id FROM ops.webhook_deliveries AS candidate
  JOIN core.repository_webhooks AS candidate_webhook ON candidate_webhook.id = candidate.webhook_id
  WHERE candidate.delivered_at IS NULL AND candidate.failed_at IS NULL
    AND candidate.available_at <= sqlc.arg(claim_time)
    AND (candidate.claimed_at IS NULL OR candidate.claimed_at < sqlc.arg(stale_before))
    AND candidate_webhook.enabled AND candidate_webhook.deleted_at IS NULL
  ORDER BY candidate.available_at, candidate.created_at, candidate.id
  FOR UPDATE OF candidate SKIP LOCKED
  LIMIT sqlc.arg(batch_size)
)
AND webhook.id = delivery.webhook_id
RETURNING delivery.*, webhook.url, webhook.secret_ciphertext;

-- name: CompleteWebhookDelivery :exec
UPDATE ops.webhook_deliveries
SET response_status = sqlc.arg(response_status), response_body = sqlc.arg(response_body),
    delivered_at = sqlc.arg(delivered_at), claimed_at = NULL, claimed_by = NULL,
    last_error_code = NULL
WHERE id = sqlc.arg(id);

-- name: RetryWebhookDelivery :exec
UPDATE ops.webhook_deliveries
SET response_status = sqlc.narg(response_status), response_body = sqlc.narg(response_body),
    available_at = sqlc.arg(available_at), claimed_at = NULL, claimed_by = NULL,
    last_error_code = sqlc.arg(last_error_code)
WHERE id = sqlc.arg(id);

-- name: FailWebhookDelivery :exec
UPDATE ops.webhook_deliveries
SET response_status = sqlc.narg(response_status), response_body = sqlc.narg(response_body),
    failed_at = sqlc.arg(failed_at), claimed_at = NULL, claimed_by = NULL,
    last_error_code = sqlc.arg(last_error_code)
WHERE id = sqlc.arg(id);

-- name: PageWebhookDeliveries :many
SELECT delivery.* FROM ops.webhook_deliveries AS delivery
JOIN core.repository_webhooks AS webhook ON webhook.id = delivery.webhook_id
WHERE delivery.webhook_id = sqlc.arg(webhook_id)
  AND webhook.repository_id = sqlc.arg(repository_id)
  AND webhook.deleted_at IS NULL
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (delivery.created_at, delivery.id) < (
      SELECT cursor.created_at, cursor.id FROM ops.webhook_deliveries AS cursor
      WHERE cursor.id = sqlc.narg(after_id)::uuid AND cursor.webhook_id = sqlc.arg(webhook_id)
    )
  )
ORDER BY delivery.created_at DESC, delivery.id DESC
LIMIT sqlc.arg(page_limit);

-- name: RedeliverWebhookDelivery :one
INSERT INTO ops.webhook_deliveries (
  id, webhook_id, event_type, event_id, request_body, available_at, created_at
)
SELECT sqlc.arg(id), original.webhook_id, original.event_type, sqlc.arg(id), original.request_body,
       sqlc.arg(created_at), sqlc.arg(created_at)
FROM ops.webhook_deliveries AS original
JOIN core.repository_webhooks AS webhook ON webhook.id = original.webhook_id
WHERE original.id = sqlc.arg(original_id)
  AND original.webhook_id = sqlc.arg(webhook_id)
  AND webhook.repository_id = sqlc.arg(repository_id)
  AND webhook.deleted_at IS NULL
RETURNING *;
