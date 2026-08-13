-- name: UpsertRepositoryCollaborator :one
INSERT INTO core.repository_collaborators (repository_id, account_did, role, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (repository_id, account_did) DO UPDATE SET
    role = EXCLUDED.role,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: GetRepositoryCollaborator :one
SELECT *
FROM core.repository_collaborators
WHERE repository_id = $1 AND account_did = $2;

-- name: PutOrganizationRepositoryCollaborator :one
INSERT INTO core.repository_collaborators (repository_id, account_did, role, created_at, updated_at)
SELECT repository.id, sqlc.arg(account_did), sqlc.arg(role), sqlc.arg(updated_at), sqlc.arg(updated_at)
FROM core.repositories AS repository
WHERE repository.id = sqlc.arg(repository_id)
  AND repository.organization_id = sqlc.arg(organization_id)
  AND repository.deleted_at IS NULL
ON CONFLICT (repository_id, account_did) DO UPDATE SET role = EXCLUDED.role, updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: ListOrganizationRepositoryCollaborators :many
SELECT collaborator.*, account.handle_cache, repository.slug AS repository_slug
FROM core.repository_collaborators AS collaborator
JOIN core.repositories AS repository ON repository.id = collaborator.repository_id
JOIN core.accounts AS account ON account.did = collaborator.account_did
WHERE repository.organization_id = sqlc.arg(organization_id)
  AND repository.id = sqlc.arg(repository_id)
  AND repository.deleted_at IS NULL
  AND (
    sqlc.narg(after_did)::text IS NULL
    OR (lower(COALESCE(account.handle_cache, collaborator.account_did)), collaborator.account_did) > (
      SELECT lower(COALESCE(cursor_account.handle_cache, cursor.account_did)), cursor.account_did
      FROM core.repository_collaborators AS cursor
      JOIN core.accounts AS cursor_account ON cursor_account.did = cursor.account_did
      WHERE cursor.repository_id = sqlc.arg(repository_id)
        AND cursor.account_did = sqlc.narg(after_did)::text
    )
  )
ORDER BY lower(COALESCE(account.handle_cache, collaborator.account_did)), collaborator.account_did
LIMIT sqlc.arg(page_limit);

-- name: DeleteOrganizationRepositoryCollaborator :execrows
DELETE FROM core.repository_collaborators AS collaborator
USING core.repositories AS repository
WHERE collaborator.repository_id = repository.id
  AND collaborator.repository_id = sqlc.arg(repository_id)
  AND collaborator.account_did = sqlc.arg(account_did)
  AND repository.organization_id = sqlc.arg(organization_id);

-- name: CanAdminOrganizationRepository :one
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
SELECT EXISTS (
  SELECT 1
  FROM core.repositories AS repository
  WHERE repository.id = sqlc.arg(repository_id)
    AND repository.organization_id = sqlc.arg(organization_id)
    AND repository.deleted_at IS NULL
    AND (
      EXISTS (
        SELECT 1
        FROM core.organization_members AS member
        WHERE member.organization_id = repository.organization_id
          AND member.account_did = sqlc.arg(account_did)
          AND member.role = 'owner'
      )
      OR EXISTS (
        SELECT 1
        FROM core.repository_collaborators AS collaborator
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
    )
);

-- name: CanWriteRepository :one
WITH RECURSIVE team_lineage AS (
  SELECT team.id, team.parent_team_id
  FROM core.organization_team_members AS team_member
  JOIN core.organization_teams AS team ON team.id = team_member.team_id
  WHERE team_member.account_did = sqlc.arg(account_did) AND team.deleted_at IS NULL
  UNION
  SELECT parent.id, parent.parent_team_id
  FROM core.organization_teams AS parent
  JOIN team_lineage AS child ON child.parent_team_id = parent.id
  WHERE parent.deleted_at IS NULL
)
SELECT EXISTS (
    SELECT 1
    FROM core.repositories AS repository
    LEFT JOIN core.repository_collaborators AS collaborator
      ON collaborator.repository_id = repository.id
     AND collaborator.account_did = sqlc.arg(account_did)
    LEFT JOIN core.organization_members AS organization_member
      ON organization_member.organization_id = repository.organization_id
     AND organization_member.account_did = sqlc.arg(account_did)
    WHERE repository.id = sqlc.arg(repository_id)
      AND repository.deleted_at IS NULL
      AND (
        (repository.organization_id IS NULL AND repository.owner_did = sqlc.arg(account_did))
        OR organization_member.role = 'owner'
        OR (organization_member.account_did IS NOT NULL AND EXISTS (
          SELECT 1 FROM core.organizations AS organization
          WHERE organization.id = repository.organization_id AND organization.base_permission = 'write'
        ))
        OR EXISTS (
          SELECT 1 FROM team_lineage
          JOIN core.organization_team_repositories AS team_repository ON team_repository.team_id = team_lineage.id
          WHERE team_repository.repository_id = repository.id
            AND team_repository.role IN ('write', 'maintain', 'admin')
        )
        OR collaborator.role IN ('write', 'maintain', 'admin')
      )
);

-- name: CanAdminRepository :one
WITH RECURSIVE team_lineage AS (
  SELECT team.id, team.parent_team_id
  FROM core.organization_team_members AS team_member
  JOIN core.organization_teams AS team ON team.id = team_member.team_id
  WHERE team_member.account_did = sqlc.arg(account_did) AND team.deleted_at IS NULL
  UNION
  SELECT parent.id, parent.parent_team_id
  FROM core.organization_teams AS parent
  JOIN team_lineage AS child ON child.parent_team_id = parent.id
  WHERE parent.deleted_at IS NULL
)
SELECT EXISTS (
  SELECT 1
  FROM core.repositories AS repository
  LEFT JOIN core.repository_collaborators AS collaborator
    ON collaborator.repository_id = repository.id
   AND collaborator.account_did = sqlc.arg(account_did)
  LEFT JOIN core.organization_members AS organization_member
    ON organization_member.organization_id = repository.organization_id
   AND organization_member.account_did = sqlc.arg(account_did)
  WHERE repository.id = sqlc.arg(repository_id)
    AND repository.deleted_at IS NULL
    AND (
      (repository.organization_id IS NULL AND repository.owner_did = sqlc.arg(account_did))
      OR organization_member.role = 'owner'
      OR EXISTS (
        SELECT 1 FROM team_lineage
        JOIN core.organization_team_repositories AS team_repository ON team_repository.team_id = team_lineage.id
        WHERE team_repository.repository_id = repository.id AND team_repository.role = 'admin'
      )
      OR collaborator.role = 'admin'
    )
);

-- name: CanTriageRepository :one
WITH RECURSIVE team_lineage AS (
  SELECT team.id, team.parent_team_id
  FROM core.organization_team_members AS team_member
  JOIN core.organization_teams AS team ON team.id = team_member.team_id
  WHERE team_member.account_did = sqlc.arg(account_did) AND team.deleted_at IS NULL
  UNION
  SELECT parent.id, parent.parent_team_id
  FROM core.organization_teams AS parent
  JOIN team_lineage AS child ON child.parent_team_id = parent.id
  WHERE parent.deleted_at IS NULL
)
SELECT EXISTS (
    SELECT 1
    FROM core.repositories AS repository
    LEFT JOIN core.repository_collaborators AS collaborator
      ON collaborator.repository_id = repository.id
     AND collaborator.account_did = sqlc.arg(account_did)
    LEFT JOIN core.organization_members AS organization_member
      ON organization_member.organization_id = repository.organization_id
     AND organization_member.account_did = sqlc.arg(account_did)
    WHERE repository.id = sqlc.arg(repository_id)
      AND repository.deleted_at IS NULL
      AND (
        (repository.organization_id IS NULL AND repository.owner_did = sqlc.arg(account_did))
        OR organization_member.role = 'owner'
        OR (organization_member.account_did IS NOT NULL AND EXISTS (
          SELECT 1 FROM core.organizations AS organization
          WHERE organization.id = repository.organization_id AND organization.base_permission = 'write'
        ))
        OR EXISTS (
          SELECT 1 FROM team_lineage
          JOIN core.organization_team_repositories AS team_repository ON team_repository.team_id = team_lineage.id
          WHERE team_repository.repository_id = repository.id
            AND team_repository.role IN ('triage', 'write', 'maintain', 'admin')
        )
        OR collaborator.role IN ('triage', 'write', 'maintain', 'admin')
      )
);

-- name: CanReadRepository :one
WITH RECURSIVE team_lineage AS (
  SELECT team.id, team.parent_team_id
  FROM core.organization_team_members AS team_member
  JOIN core.organization_teams AS team ON team.id = team_member.team_id
  WHERE team_member.account_did = sqlc.arg(account_did) AND team.deleted_at IS NULL
  UNION
  SELECT parent.id, parent.parent_team_id
  FROM core.organization_teams AS parent
  JOIN team_lineage AS child ON child.parent_team_id = parent.id
  WHERE parent.deleted_at IS NULL
)
SELECT EXISTS (
    SELECT 1
    FROM core.repositories AS repository
    LEFT JOIN core.repository_collaborators AS collaborator
      ON collaborator.repository_id = repository.id
     AND collaborator.account_did = sqlc.arg(account_did)
    LEFT JOIN core.organization_members AS organization_member
      ON organization_member.organization_id = repository.organization_id
     AND organization_member.account_did = sqlc.arg(account_did)
    WHERE repository.id = sqlc.arg(repository_id)
      AND repository.deleted_at IS NULL
      AND (
        repository.visibility = 'public'
        OR (repository.organization_id IS NULL AND repository.owner_did = sqlc.arg(account_did))
        OR organization_member.role = 'owner'
        OR (organization_member.account_did IS NOT NULL AND EXISTS (
          SELECT 1 FROM core.organizations AS organization
          WHERE organization.id = repository.organization_id AND organization.base_permission IN ('read', 'write')
        ))
        OR EXISTS (
          SELECT 1 FROM team_lineage
          JOIN core.organization_team_repositories AS team_repository ON team_repository.team_id = team_lineage.id
          WHERE team_repository.repository_id = repository.id
            AND team_repository.role IN ('read', 'triage', 'write', 'maintain', 'admin')
        )
        OR collaborator.role IN ('read', 'triage', 'write', 'maintain', 'admin')
      )
);
