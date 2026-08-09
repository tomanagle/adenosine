-- name: UpsertAccount :one
INSERT INTO core.accounts (did, handle_cache, first_seen_at, last_seen_at, last_login_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (did) DO UPDATE SET
    handle_cache = EXCLUDED.handle_cache,
    last_seen_at = EXCLUDED.last_seen_at,
    last_login_at = COALESCE(EXCLUDED.last_login_at, core.accounts.last_login_at)
RETURNING *;

-- name: GetAccount :one
SELECT * FROM core.accounts WHERE did = $1;

-- name: TouchAccountLogin :one
UPDATE core.accounts
SET last_seen_at = $2,
    last_login_at = $2
WHERE did = $1
RETURNING *;
