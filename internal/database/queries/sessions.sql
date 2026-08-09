-- name: CreateSession :one
INSERT INTO auth.sessions (id, account_did, token_hash, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: AuthenticateSession :one
UPDATE auth.sessions
SET last_seen_at = sqlc.arg(seen_at)
WHERE token_hash = sqlc.arg(token_hash)
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(seen_at)
RETURNING *;

-- name: RevokeSession :one
UPDATE auth.sessions
SET revoked_at = sqlc.arg(revoked_at)
WHERE id = sqlc.arg(id)
  AND account_did = sqlc.arg(account_did)
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(revoked_at)
RETURNING *;
