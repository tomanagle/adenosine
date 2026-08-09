-- name: UpsertOAuthCredential :exec
INSERT INTO auth.oauth_credentials (
    account_did,
    session_id_hash,
    encrypted_payload,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (account_did, session_id_hash) DO UPDATE
SET encrypted_payload = EXCLUDED.encrypted_payload,
    updated_at = EXCLUDED.updated_at;

-- name: GetOAuthCredential :one
SELECT encrypted_payload
FROM auth.oauth_credentials
WHERE account_did = $1
  AND session_id_hash = $2;

-- name: GetLatestOAuthCredential :one
SELECT session_id_hash, encrypted_payload
FROM auth.oauth_credentials
WHERE account_did = $1
ORDER BY updated_at DESC
LIMIT 1;

-- name: DeleteOAuthCredential :exec
DELETE FROM auth.oauth_credentials
WHERE account_did = $1
  AND session_id_hash = $2;
