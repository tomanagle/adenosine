-- name: UpsertAccount :one
WITH account AS (
    INSERT INTO core.accounts (did, handle_cache, first_seen_at, last_seen_at, last_login_at, created_at)
    VALUES ($1, $2, $3, $4, $5, $6)
    ON CONFLICT (did) DO UPDATE SET
        handle_cache = EXCLUDED.handle_cache,
        last_seen_at = EXCLUDED.last_seen_at,
        last_login_at = COALESCE(EXCLUDED.last_login_at, core.accounts.last_login_at)
    RETURNING *
), owner_route AS (
    DELETE FROM core.owner_routes AS route
    USING account
    WHERE route.kind = 'account'
      AND route.account_did = account.did
      AND lower(route.alias) IS DISTINCT FROM lower(account.handle_cache)
    RETURNING route.alias
), current_owner_route AS (
    INSERT INTO core.owner_routes (alias, kind, account_did, created_at)
    SELECT lower(handle_cache), 'account', did, created_at
    FROM account
    CROSS JOIN (SELECT count(*) FROM owner_route) AS removed
    WHERE handle_cache IS NOT NULL AND handle_cache <> ''
    ON CONFLICT (alias) DO UPDATE SET account_did = EXCLUDED.account_did
    WHERE core.owner_routes.kind = 'account'
)
SELECT * FROM account;

-- name: GetAccount :one
SELECT * FROM core.accounts WHERE did = $1;

-- name: EnsureAccount :exec
INSERT INTO core.accounts (did, first_seen_at, last_seen_at, created_at)
VALUES (sqlc.arg(did), sqlc.arg(seen_at), sqlc.arg(seen_at), sqlc.arg(seen_at))
ON CONFLICT (did) DO NOTHING;

-- name: TouchAccountLogin :one
UPDATE core.accounts
SET last_seen_at = $2,
    last_login_at = $2
WHERE did = $1
RETURNING *;
