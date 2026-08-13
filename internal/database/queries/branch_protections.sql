-- name: CreateBranchProtection :one
INSERT INTO core.branch_protections (
  id, repository_id, pattern, deny_force_push, deny_deletion, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- name: GetBranchProtection :one
SELECT * FROM core.branch_protections WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id);

-- name: PageBranchProtections :many
SELECT * FROM core.branch_protections AS protection
WHERE protection.repository_id = sqlc.arg(repository_id)
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (protection.created_at, protection.id) < (
      SELECT cursor.created_at, cursor.id FROM core.branch_protections AS cursor
      WHERE cursor.id = sqlc.narg(after_id)::uuid AND cursor.repository_id = sqlc.arg(repository_id)
    )
  )
ORDER BY protection.created_at DESC, protection.id DESC
LIMIT sqlc.arg(page_limit);

-- name: UpdateBranchProtection :one
UPDATE core.branch_protections
SET pattern = sqlc.arg(pattern), deny_force_push = sqlc.arg(deny_force_push),
    deny_deletion = sqlc.arg(deny_deletion), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id)
RETURNING *;

-- name: DeleteBranchProtection :execrows
DELETE FROM core.branch_protections WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id);

-- name: GetEffectiveReceiveProtection :one
SELECT coalesce(bool_or(deny_force_push), false)::boolean AS deny_force_push,
       coalesce(bool_or(deny_deletion), false)::boolean AS deny_deletion
FROM core.branch_protections
WHERE repository_id = $1;
