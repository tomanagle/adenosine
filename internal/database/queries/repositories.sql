-- name: CreateRepository :one
INSERT INTO core.repositories (
    id, owner_did, organization_id, slug, display_name, description, visibility, state,
    default_branch, storage_key, at_uri, at_cid, forked_from_uri, forked_from_cid,
    forked_from_local_repository_id, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
RETURNING *;

-- name: GetRepository :one
SELECT * FROM core.repositories WHERE id = $1 AND deleted_at IS NULL;

-- name: GetForkSourceByURI :one
SELECT repository.uri, repository.cid, repository.git_https, repository.local_repository_id
FROM network.repositories AS repository
LEFT JOIN core.repositories AS local_repository ON local_repository.id = repository.local_repository_id
WHERE repository.uri = $1
  AND repository.cid IS NOT NULL
  AND repository.deleted_at IS NULL
  AND (local_repository.id IS NULL OR (
      local_repository.visibility = 'public'
      AND local_repository.state = 'active'
      AND local_repository.deleted_at IS NULL
  ));

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
LEFT JOIN core.owner_routes AS owner_route ON (
    (repository.organization_id IS NULL AND owner_route.account_did = repository.owner_did)
    OR owner_route.organization_id = repository.organization_id
)
LEFT JOIN core.repository_aliases AS repository_alias
  ON repository_alias.repository_id = repository.id
WHERE (
    (
      (
        lower(owner_route.alias) = lower(sqlc.arg(owner))
        OR (repository.organization_id IS NULL AND repository.owner_did = sqlc.arg(owner))
      )
      AND lower(repository.slug) = lower(sqlc.arg(slug))
    )
    OR (
      lower(repository_alias.owner_alias) = lower(sqlc.arg(owner))
      AND lower(repository_alias.slug_alias) = lower(sqlc.arg(slug))
    )
  )
  AND repository.deleted_at IS NULL;

-- name: UpdateRepositorySettings :one
WITH previous AS (
  SELECT repository.id, repository.slug
  FROM core.repositories AS repository
  WHERE repository.id = sqlc.arg(id)
    AND repository.deleted_at IS NULL
    AND repository.state = 'active'
), alias AS (
  INSERT INTO core.repository_aliases (id, repository_id, owner_alias, slug_alias, created_at)
  SELECT sqlc.arg(alias_id), previous.id, sqlc.arg(owner_alias), previous.slug, sqlc.arg(updated_at)
  FROM previous
  WHERE lower(previous.slug) <> lower(sqlc.arg(slug))
  ON CONFLICT (lower(owner_alias), lower(slug_alias)) DO NOTHING
)
UPDATE core.repositories AS repository
SET slug = sqlc.arg(slug),
    display_name = sqlc.narg(display_name),
    description = sqlc.narg(description),
    visibility = sqlc.arg(visibility),
    default_branch = sqlc.arg(default_branch),
    archived_at = sqlc.narg(archived_at),
    at_uri = sqlc.narg(at_uri),
    at_cid = sqlc.narg(at_cid),
    updated_at = sqlc.arg(updated_at)
FROM previous
WHERE repository.id = previous.id
RETURNING repository.*;

-- name: RequestRepositoryDeletion :one
WITH changed AS (
  UPDATE core.repositories AS repository
  SET state = 'deleting', updated_at = sqlc.arg(requested_at)
  WHERE repository.id = sqlc.arg(repository_id)
    AND repository.state = 'active'
    AND repository.deleted_at IS NULL
  RETURNING repository.id
)
INSERT INTO core.repository_deletions (
  id, repository_id, requested_by_did, requested_at, purge_after
)
SELECT sqlc.arg(id), changed.id, sqlc.arg(requested_by_did), sqlc.arg(requested_at), sqlc.arg(purge_after)
FROM changed
RETURNING *;

-- name: GetRepositoryDeletion :one
SELECT deletion.*
FROM core.repository_deletions AS deletion
JOIN core.repositories AS repository ON repository.id = deletion.repository_id
WHERE deletion.id = $1
  AND deletion.restored_at IS NULL
  AND deletion.purged_at IS NULL;

-- name: RestoreRepositoryDeletion :one
WITH restored AS (
  UPDATE core.repository_deletions AS deletion
  SET restored_at = sqlc.arg(restored_at)
  WHERE deletion.id = sqlc.arg(id)
    AND deletion.restored_at IS NULL
    AND deletion.purged_at IS NULL
    AND deletion.purge_after > sqlc.arg(restored_at)
  RETURNING deletion.repository_id
)
UPDATE core.repositories AS repository
SET state = 'active', updated_at = sqlc.arg(restored_at)
FROM restored
WHERE repository.id = restored.repository_id
RETURNING repository.*;

-- name: ListDueRepositoryDeletions :many
SELECT deletion.*
FROM core.repository_deletions AS deletion
WHERE deletion.restored_at IS NULL
  AND deletion.purged_at IS NULL
  AND deletion.purge_after <= sqlc.arg(now)
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (deletion.purge_after, deletion.id) > (
      SELECT cursor.purge_after, cursor.id
      FROM core.repository_deletions AS cursor
      WHERE cursor.id = sqlc.narg(after_id)::uuid
    )
  )
ORDER BY deletion.purge_after, deletion.id
LIMIT sqlc.arg(page_limit);

-- name: MarkRepositoryPurged :exec
WITH purged AS (
  UPDATE core.repository_deletions AS deletion
  SET purged_at = sqlc.arg(purged_at)
  WHERE deletion.id = sqlc.arg(id)
    AND deletion.restored_at IS NULL
    AND deletion.purged_at IS NULL
  RETURNING deletion.repository_id
)
UPDATE core.repositories AS repository
SET state = 'deleted', deleted_at = sqlc.arg(purged_at), updated_at = sqlc.arg(purged_at)
FROM purged
WHERE repository.id = purged.repository_id;

-- name: ListRepositoriesByOwner :many
SELECT *
FROM core.repositories
WHERE owner_did = $1 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: ListRepositoriesByOrganization :many
SELECT *
FROM core.repositories
WHERE organization_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: PageRepositoriesByOrganization :many
WITH RECURSIVE team_lineage AS (
  SELECT team.id, team.parent_team_id
  FROM core.organization_team_members AS team_member
  JOIN core.organization_teams AS team ON team.id = team_member.team_id
  WHERE team_member.account_did = sqlc.arg(account_did)
    AND team.organization_id = sqlc.arg(organization_id)
    AND team.deleted_at IS NULL
  UNION
  SELECT parent.id, parent.parent_team_id
  FROM core.organization_teams AS parent
  JOIN team_lineage AS child ON child.parent_team_id = parent.id
  WHERE parent.organization_id = sqlc.arg(organization_id)
    AND parent.deleted_at IS NULL
)
SELECT repository.*,
  (
    EXISTS (
      SELECT 1 FROM core.organization_members AS member
      WHERE member.organization_id = repository.organization_id
        AND member.account_did = sqlc.arg(account_did)
        AND member.role = 'owner'
    )
    OR EXISTS (
      SELECT 1 FROM core.repository_collaborators AS collaborator
      WHERE collaborator.repository_id = repository.id
        AND collaborator.account_did = sqlc.arg(account_did)
        AND collaborator.role = 'admin'
    )
    OR EXISTS (
      SELECT 1
      FROM team_lineage
      JOIN core.organization_team_repositories AS team_repository
        ON team_repository.team_id = team_lineage.id
      WHERE team_repository.repository_id = repository.id
        AND team_repository.role = 'admin'
    )
  ) AS viewer_can_admin
FROM core.repositories AS repository
WHERE repository.organization_id = sqlc.arg(organization_id)
  AND repository.deleted_at IS NULL
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (repository.created_at, repository.id) < (
      SELECT cursor.created_at, cursor.id
      FROM core.repositories AS cursor
      WHERE cursor.organization_id = sqlc.arg(organization_id)
        AND cursor.id = sqlc.narg(after_id)::uuid
    )
  )
ORDER BY repository.created_at DESC, repository.id DESC
LIMIT sqlc.arg(page_limit);
