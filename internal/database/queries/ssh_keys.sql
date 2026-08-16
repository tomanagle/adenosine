-- name: CreateSSHKey :one
INSERT INTO auth.ssh_keys (
    id, account_did, name, algorithm, public_key, fingerprint, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetActiveSSHKeyByFingerprint :one
SELECT *
FROM auth.ssh_keys
WHERE fingerprint = $1
  AND revoked_at IS NULL;

-- name: TouchSSHKey :exec
UPDATE auth.ssh_keys SET last_used_at = $2 WHERE id = $1;

-- name: ListActiveSSHKeysByAccountDID :many
SELECT *
FROM auth.ssh_keys
WHERE account_did = $1
  AND revoked_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: RevokeSSHKey :one
UPDATE auth.ssh_keys
SET revoked_at = sqlc.arg(revoked_at)
WHERE id = sqlc.arg(id)
  AND account_did = sqlc.arg(account_did)
  AND revoked_at IS NULL
RETURNING *;

-- name: ListActiveSSHKeysForCommitVerification :many
SELECT account_did, public_key
FROM auth.ssh_keys
WHERE revoked_at IS NULL
ORDER BY account_did, id;
