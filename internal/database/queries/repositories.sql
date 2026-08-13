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
WHERE (
    lower(owner_route.alias) = lower(sqlc.arg(owner))
    OR (repository.organization_id IS NULL AND repository.owner_did = sqlc.arg(owner))
  )
  AND lower(repository.slug) = lower(sqlc.arg(slug))
  AND repository.deleted_at IS NULL;

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
