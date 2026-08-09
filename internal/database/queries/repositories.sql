-- name: CreateRepository :one
INSERT INTO core.repositories (
    id, owner_did, slug, display_name, description, visibility, state,
    default_branch, storage_key, at_uri, at_cid, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: GetRepository :one
SELECT * FROM core.repositories WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateRepositoryState :one
UPDATE core.repositories
SET state = $2, updated_at = $3
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: ActivateRepository :one
UPDATE core.repositories
SET state = 'active', at_uri = sqlc.narg(at_uri), at_cid = sqlc.narg(at_cid), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND deleted_at IS NULL
RETURNING *;

-- name: GetRepositoryByOwnerSlug :one
SELECT repository.*
FROM core.repositories AS repository
JOIN core.accounts AS owner ON owner.did = repository.owner_did
WHERE (repository.owner_did = $1 OR lower(owner.handle_cache) = lower($1))
  AND lower(repository.slug) = lower($2)
  AND repository.deleted_at IS NULL;

-- name: ListRepositoriesByOwner :many
SELECT *
FROM core.repositories
WHERE owner_did = $1 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT $2;
