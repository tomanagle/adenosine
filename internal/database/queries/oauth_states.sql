-- name: CreateOAuthState :exec
INSERT INTO auth.oauth_states (state_hash, encrypted_payload, created_at, expires_at)
VALUES ($1, $2, $3, $4);

-- name: ConsumeOAuthState :one
DELETE FROM auth.oauth_states
WHERE state_hash = $1
  AND expires_at > $2
RETURNING encrypted_payload;

-- name: DeleteOAuthState :exec
DELETE FROM auth.oauth_states
WHERE state_hash = $1;
