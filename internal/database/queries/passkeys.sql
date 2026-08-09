-- name: CreateWebAuthnUser :one
INSERT INTO auth.webauthn_users (rp_id, account_did, name, display_name, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (rp_id, account_did) DO UPDATE SET
    account_did = auth.webauthn_users.account_did
RETURNING *;

-- name: GetWebAuthnUser :one
SELECT *
FROM auth.webauthn_users
WHERE rp_id = $1
  AND account_did = $2;

-- name: CreatePasskeyCredential :one
INSERT INTO auth.passkey_credentials (
    id, rp_id, account_did, name, credential_id, public_key,
    attestation_type, transports, flags, aaguid, sign_count, clone_warning,
    attachment, attestation_client_data_json, attestation_client_data_hash,
    attestation_authenticator_data, attestation_public_key_algorithm,
    attestation_object, created_at
)
VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12,
    $13, $14, $15, $16, $17, $18, $19
)
RETURNING *;

-- name: GetActivePasskeyCredentialByCredentialID :one
SELECT *
FROM auth.passkey_credentials
WHERE rp_id = $1
  AND credential_id = $2
  AND revoked_at IS NULL;

-- name: UpdatePasskeyCredential :one
UPDATE auth.passkey_credentials
SET sign_count = GREATEST(sign_count, sqlc.arg(sign_count)),
    flags = sqlc.arg(flags),
    clone_warning = clone_warning
        OR sqlc.arg(clone_warning)
        OR (sign_count <> 0 AND sqlc.arg(sign_count) <= sign_count),
    last_used_at = CASE
        WHEN last_used_at IS NULL THEN sqlc.arg(last_used_at)
        ELSE GREATEST(last_used_at, sqlc.arg(last_used_at))
    END
WHERE id = sqlc.arg(id)
  AND rp_id = sqlc.arg(rp_id)
  AND account_did = sqlc.arg(account_did)
  AND revoked_at IS NULL
RETURNING *;

-- name: ListActivePasskeyCredentials :many
SELECT *
FROM auth.passkey_credentials
WHERE rp_id = $1
  AND account_did = $2
  AND revoked_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: RevokePasskeyCredential :one
UPDATE auth.passkey_credentials
SET revoked_at = sqlc.arg(revoked_at)
WHERE id = sqlc.arg(id)
  AND rp_id = sqlc.arg(rp_id)
  AND account_did = sqlc.arg(account_did)
  AND revoked_at IS NULL
RETURNING *;

-- name: CreatePasskeyCeremony :one
INSERT INTO auth.passkey_ceremonies (
    token_hash, kind, rp_id, account_did, browser_session_id,
    session_data, created_at, expires_at
)
VALUES (
    sqlc.arg(token_hash), sqlc.arg(kind), sqlc.arg(rp_id),
    NULLIF(sqlc.arg(account_did)::text, ''), sqlc.narg(browser_session_id)::uuid,
    sqlc.arg(session_data), sqlc.arg(created_at), sqlc.arg(expires_at)
)
RETURNING token_hash, kind, rp_id, COALESCE(account_did, '') AS account_did,
    browser_session_id, session_data, created_at, expires_at;

-- name: PurgeExpiredPasskeyCeremonies :execrows
DELETE FROM auth.passkey_ceremonies
WHERE expires_at <= $1;

-- name: ConsumePasskeyCeremony :one
DELETE FROM auth.passkey_ceremonies
WHERE token_hash = $1
  AND expires_at > $2
RETURNING token_hash, kind, rp_id, COALESCE(account_did, '') AS account_did,
    browser_session_id, session_data, created_at, expires_at;
