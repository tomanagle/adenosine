-- name: CreateRelease :one
INSERT INTO core.releases (
  id, repository_id, tag_name, target_sha, name, body, state, prerelease,
  created_by_did, created_at, updated_at, published_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $11)
RETURNING *;

-- name: GetRelease :one
SELECT * FROM core.releases
WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id);

-- name: PageReleases :many
SELECT * FROM core.releases AS release
WHERE release.repository_id = sqlc.arg(repository_id)
  AND (release.state = 'published' OR (sqlc.arg(include_drafts)::boolean AND release.state = 'draft'))
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (release.created_at, release.id) < (
      SELECT cursor.created_at, cursor.id FROM core.releases AS cursor
      WHERE cursor.id = sqlc.narg(after_id)::uuid
        AND cursor.repository_id = sqlc.arg(repository_id)
    )
  )
ORDER BY release.created_at DESC, release.id DESC
LIMIT sqlc.arg(page_limit);

-- name: UpdateRelease :one
UPDATE core.releases
SET name = sqlc.arg(name), body = sqlc.arg(body), state = sqlc.arg(state),
    prerelease = sqlc.arg(prerelease), updated_at = sqlc.arg(updated_at),
    published_at = sqlc.narg(published_at)
WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id)
RETURNING *;

-- name: MarkReleaseDeleting :one
UPDATE core.releases
SET state = 'deleting', updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id)
RETURNING *;

-- name: DeleteRelease :execrows
DELETE FROM core.releases
WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id);

-- name: CreateReleaseAsset :one
INSERT INTO core.release_assets (
  id, release_id, repository_id, name, content_type, size_bytes, sha256,
  storage_key, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: LockReleaseAssetQuota :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(repository_id)::text, 0));

-- name: GetReleaseAsset :one
SELECT * FROM core.release_assets
WHERE id = sqlc.arg(id) AND release_id = sqlc.arg(release_id)
  AND repository_id = sqlc.arg(repository_id);

-- name: PageReleaseAssets :many
SELECT * FROM core.release_assets AS asset
WHERE asset.release_id = sqlc.arg(release_id)
  AND asset.repository_id = sqlc.arg(repository_id)
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (asset.created_at, asset.id) < (
      SELECT cursor.created_at, cursor.id FROM core.release_assets AS cursor
      WHERE cursor.id = sqlc.narg(after_id)::uuid
        AND cursor.release_id = sqlc.arg(release_id)
        AND cursor.repository_id = sqlc.arg(repository_id)
    )
  )
ORDER BY asset.created_at DESC, asset.id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListReleaseAssetsForDeletion :many
SELECT * FROM core.release_assets
WHERE release_id = sqlc.arg(release_id) AND repository_id = sqlc.arg(repository_id)
ORDER BY id;

-- name: GetReleaseAssetUsage :one
SELECT
  coalesce(sum(size_bytes) FILTER (WHERE release_id = sqlc.arg(release_id)), 0)::bigint AS release_bytes,
  coalesce(sum(size_bytes), 0)::bigint AS repository_bytes
FROM core.release_assets
WHERE repository_id = sqlc.arg(repository_id);

-- name: DeleteReleaseAsset :execrows
DELETE FROM core.release_assets
WHERE id = sqlc.arg(id) AND release_id = sqlc.arg(release_id)
  AND repository_id = sqlc.arg(repository_id);
