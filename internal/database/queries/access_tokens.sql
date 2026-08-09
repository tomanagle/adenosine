-- name: CreateAccessToken :one
INSERT INTO auth.access_tokens (
    id, account_did, name, token_prefix, token_hash, scopes, repository_id,
    created_at, expires_at, last_used_at, revoked_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetActiveAccessTokenByHash :one
SELECT *
FROM auth.access_tokens
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now());

-- name: TouchAccessToken :exec
UPDATE auth.access_tokens SET last_used_at = $2 WHERE id = $1;

-- name: ListActiveAccessTokensByAccountDID :many
SELECT id, account_did, name, token_prefix, scopes, repository_id,
       created_at, expires_at, last_used_at, revoked_at
FROM auth.access_tokens
WHERE account_did = sqlc.arg(account_did)
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > sqlc.arg(active_at))
ORDER BY created_at DESC, id DESC;

-- name: RevokeAccessToken :one
UPDATE auth.access_tokens
SET revoked_at = sqlc.arg(revoked_at)
WHERE id = sqlc.arg(id)
  AND account_did = sqlc.arg(account_did)
  AND revoked_at IS NULL
RETURNING *;
