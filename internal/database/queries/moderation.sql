-- name: BlockDID :exec
INSERT INTO moderation.blocked_dids (account_did, blocked_did, created_at)
VALUES ($1, $2, $3)
ON CONFLICT (account_did, blocked_did) DO NOTHING;

-- name: UnblockDID :exec
DELETE FROM moderation.blocked_dids
WHERE account_did = $1 AND blocked_did = $2;

-- name: ListBlockedDIDs :many
SELECT blocked_did, created_at
FROM moderation.blocked_dids
WHERE account_did = $1
ORDER BY created_at DESC, blocked_did;

-- name: IsDIDBlocked :one
SELECT EXISTS (
    SELECT 1 FROM moderation.blocked_dids
    WHERE account_did = $1 AND blocked_did = $2
);

-- name: HideRecord :exec
INSERT INTO moderation.hidden_records (account_did, record_uri, created_at)
VALUES ($1, $2, $3)
ON CONFLICT (account_did, record_uri) DO NOTHING;

-- name: UnhideRecord :exec
DELETE FROM moderation.hidden_records
WHERE account_did = $1 AND record_uri = $2;

-- name: ListHiddenRecords :many
SELECT record_uri, created_at
FROM moderation.hidden_records
WHERE account_did = $1
ORDER BY created_at DESC, record_uri;

-- name: IsRecordHidden :one
SELECT EXISTS (
    SELECT 1 FROM moderation.hidden_records
    WHERE account_did = $1 AND record_uri = $2
);
