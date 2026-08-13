-- name: CreateOrganization :one
INSERT INTO core.organizations (
    id, slug, name, description, website, location, creator_did,
    base_permission, members_can_create_repositories, state, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, 'creating', $10, $10
)
RETURNING *;

-- name: ActivateOrganization :one
UPDATE core.organizations
SET state = 'active', at_uri = $2, at_cid = $3, updated_at = $4
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: FailOrganization :one
UPDATE core.organizations
SET state = 'failed', updated_at = $2
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: GetOrganizationByID :one
SELECT *
FROM core.organizations
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetOrganizationBySlug :one
SELECT *
FROM core.organizations
WHERE lower(slug) = lower($1) AND deleted_at IS NULL;

-- name: UpdateOrganization :one
UPDATE core.organizations
SET name = sqlc.arg(name),
    description = sqlc.arg(description),
    website = sqlc.arg(website),
    location = sqlc.arg(location),
    base_permission = sqlc.arg(base_permission),
    members_can_create_repositories = sqlc.arg(members_can_create_repositories),
    at_cid = sqlc.arg(at_cid),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND state = 'active' AND deleted_at IS NULL
RETURNING *;

-- name: ListOrganizationsForAccount :many
SELECT organization.*
FROM core.organizations AS organization
JOIN core.organization_members AS member ON member.organization_id = organization.id
WHERE member.account_did = $1
  AND organization.state = 'active'
  AND organization.deleted_at IS NULL
ORDER BY lower(organization.name), organization.id;

-- name: PageOrganizationsForAccount :many
SELECT organization.*
FROM core.organizations AS organization
JOIN core.organization_members AS member ON member.organization_id = organization.id
WHERE member.account_did = sqlc.arg(account_did)
  AND organization.state = 'active'
  AND organization.deleted_at IS NULL
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR organization.id < sqlc.narg(after_id)::uuid
  )
ORDER BY organization.id DESC
LIMIT sqlc.arg(page_limit);

-- name: CreateOrganizationOwner :one
INSERT INTO core.organization_members (
    organization_id, account_did, role, visibility, invited_by_did, joined_at, updated_at
) VALUES ($1, $2, 'owner', 'private', $2, $3, $3)
RETURNING *;

-- name: GetOrganizationMember :one
SELECT *
FROM core.organization_members
WHERE organization_id = $1 AND account_did = $2;

-- name: ListOrganizationMembers :many
SELECT member.*, account.handle_cache
FROM core.organization_members AS member
JOIN core.accounts AS account ON account.did = member.account_did
WHERE member.organization_id = $1
ORDER BY CASE member.role WHEN 'owner' THEN 0 ELSE 1 END,
         lower(COALESCE(account.handle_cache, member.account_did));

-- name: PageOrganizationMembers :many
SELECT member.*, account.handle_cache
FROM core.organization_members AS member
JOIN core.accounts AS account ON account.did = member.account_did
WHERE member.organization_id = sqlc.arg(organization_id)
  AND (sqlc.arg(include_private)::boolean OR member.visibility = 'public')
  AND (sqlc.arg(after_did)::text = '' OR member.account_did > sqlc.arg(after_did)::text)
ORDER BY member.account_did
LIMIT sqlc.arg(page_limit);

-- name: LockOrganizationOwners :many
SELECT account_did
FROM core.organization_members
WHERE organization_id = $1 AND role = 'owner'
FOR UPDATE;

-- name: GetOrganizationOwner :one
SELECT member.*
FROM core.organization_members AS member
JOIN core.organizations AS organization ON organization.id = member.organization_id
WHERE member.organization_id = $1
  AND member.account_did = $2
  AND member.role = 'owner'
  AND organization.state = 'active'
  AND organization.deleted_at IS NULL;

-- name: CreateOrganizationInvitation :one
INSERT INTO core.organization_invitations (
    id, organization_id, invitee_did, role, invited_by_did, created_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: SetOrganizationInvitationGrant :one
UPDATE core.organization_invitations
SET grant_uri = $2, grant_cid = $3
WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL
RETURNING *;

-- name: GetOrganizationInvitation :one
SELECT *
FROM core.organization_invitations
WHERE id = $1;

-- name: LockOrganizationInvitation :one
SELECT *
FROM core.organization_invitations
WHERE id = $1
FOR UPDATE;

-- name: RevokeOrganizationInvitation :one
UPDATE core.organization_invitations
SET revoked_at = $2
WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL
RETURNING *;

-- name: AcceptOrganizationInvitation :one
UPDATE core.organization_invitations
SET accepted_at = $2
WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $2
RETURNING *;

-- name: CreateOrganizationMemberFromInvitation :one
INSERT INTO core.organization_members (
    organization_id, account_did, role, visibility, invited_by_did,
    grant_uri, grant_cid, membership_uri, membership_cid, joined_at, updated_at
) VALUES ($1, $2, $3, 'private', $4, $5, $6, $7, $8, $9, $9)
RETURNING *;

-- name: UpdateOrganizationMemberRole :one
UPDATE core.organization_members
SET role = sqlc.arg(role),
    grant_uri = sqlc.narg(grant_uri),
    grant_cid = sqlc.narg(grant_cid),
    membership_uri = sqlc.narg(membership_uri),
    membership_cid = sqlc.narg(membership_cid),
    updated_at = sqlc.arg(updated_at)
WHERE organization_id = $1 AND account_did = $2
RETURNING *;

-- name: DeleteOrganizationMember :one
DELETE FROM core.organization_members
WHERE organization_id = $1 AND account_did = $2
RETURNING *;

-- name: ListOrganizationInvitations :many
SELECT *
FROM core.organization_invitations
WHERE organization_id = $1
  AND accepted_at IS NULL
  AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: PageOrganizationInvitations :many
SELECT invitation.*
FROM core.organization_invitations AS invitation
WHERE invitation.organization_id = sqlc.arg(organization_id)
  AND invitation.accepted_at IS NULL
  AND invitation.revoked_at IS NULL
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (invitation.created_at, invitation.id) < (
      SELECT cursor.created_at, cursor.id
      FROM core.organization_invitations AS cursor
      WHERE cursor.organization_id = sqlc.arg(organization_id)
        AND cursor.id = sqlc.narg(after_id)::uuid
    )
  )
ORDER BY invitation.created_at DESC, invitation.id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListPendingOrganizationInvitationsForAccount :many
SELECT invitation.*, organization.slug AS organization_slug, organization.name AS organization_name
FROM core.organization_invitations AS invitation
JOIN core.organizations AS organization ON organization.id = invitation.organization_id
WHERE invitation.invitee_did = $1
  AND invitation.accepted_at IS NULL
  AND invitation.revoked_at IS NULL
  AND invitation.expires_at > $2
  AND organization.state = 'active'
  AND organization.deleted_at IS NULL
ORDER BY invitation.created_at DESC;

-- name: PagePendingOrganizationInvitationsForAccount :many
SELECT invitation.*, organization.slug AS organization_slug, organization.name AS organization_name
FROM core.organization_invitations AS invitation
JOIN core.organizations AS organization ON organization.id = invitation.organization_id
WHERE invitation.invitee_did = sqlc.arg(invitee_did)
  AND invitation.accepted_at IS NULL
  AND invitation.revoked_at IS NULL
  AND invitation.expires_at > sqlc.arg(expires_at)
  AND organization.state = 'active'
  AND organization.deleted_at IS NULL
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (invitation.created_at, invitation.id) < (
      SELECT cursor.created_at, cursor.id
      FROM core.organization_invitations AS cursor
      WHERE cursor.invitee_did = invitation.invitee_did
        AND cursor.id = sqlc.narg(after_id)::uuid
    )
  )
ORDER BY invitation.created_at DESC, invitation.id DESC
LIMIT sqlc.arg(page_limit);

-- name: UpdateOrganizationMembershipVisibility :one
UPDATE core.organization_members
SET visibility = sqlc.arg(visibility),
    grant_uri = sqlc.narg(grant_uri),
    grant_cid = sqlc.narg(grant_cid),
    membership_uri = sqlc.narg(membership_uri),
    membership_cid = sqlc.narg(membership_cid),
    updated_at = sqlc.arg(updated_at)
WHERE organization_id = sqlc.arg(organization_id) AND account_did = sqlc.arg(account_did)
RETURNING *;

-- name: InsertOrganizationAuditEvent :exec
INSERT INTO ops.organization_audit_events (
    id, organization_id, actor_did, action, target_type, target_id, request_id, metadata, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListOrganizationAuditEvents :many
SELECT audit.*
FROM ops.organization_audit_events AS audit
WHERE audit.organization_id = sqlc.arg(organization_id)
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (audit.created_at, audit.id) < (
      SELECT cursor.created_at, cursor.id
      FROM ops.organization_audit_events AS cursor
      WHERE cursor.organization_id = sqlc.arg(organization_id)
        AND cursor.id = sqlc.narg(after_id)::uuid
    )
  )
ORDER BY audit.created_at DESC, audit.id DESC
LIMIT sqlc.arg(page_limit);

-- name: CreateOrganizationTeam :one
INSERT INTO core.organization_teams (
    id, organization_id, parent_team_id, slug, name, description, visibility, created_at, updated_at
)
SELECT sqlc.arg(id), sqlc.arg(organization_id), sqlc.narg(parent_team_id), sqlc.arg(slug),
       sqlc.arg(name), sqlc.arg(description), sqlc.arg(visibility), sqlc.arg(created_at), sqlc.arg(created_at)
WHERE sqlc.narg(parent_team_id)::uuid IS NULL OR (
  sqlc.arg(visibility)::text = 'visible'
  AND EXISTS (
    SELECT 1 FROM core.organization_teams AS parent
    WHERE parent.id = sqlc.narg(parent_team_id)::uuid
      AND parent.organization_id = sqlc.arg(organization_id)
      AND parent.visibility = 'visible'
      AND parent.deleted_at IS NULL
  )
)
RETURNING *;

-- name: ListOrganizationTeams :many
SELECT team.*,
       EXISTS (
         SELECT 1 FROM core.organization_team_members AS team_member
         WHERE team_member.team_id = team.id AND team_member.account_did = sqlc.arg(viewer_did)
       ) AS viewer_is_member,
       COALESCE((
         SELECT team_member.role FROM core.organization_team_members AS team_member
         WHERE team_member.team_id = team.id AND team_member.account_did = sqlc.arg(viewer_did)
       ), '')::text AS viewer_role
FROM core.organization_teams AS team
WHERE team.organization_id = sqlc.arg(organization_id)
  AND team.deleted_at IS NULL
ORDER BY lower(team.name), team.id;

-- name: PageOrganizationTeams :many
SELECT team.*,
       EXISTS (
         SELECT 1 FROM core.organization_team_members AS team_member
         WHERE team_member.team_id = team.id AND team_member.account_did = sqlc.arg(viewer_did)
       ) AS viewer_is_member,
       COALESCE((
         SELECT team_member.role FROM core.organization_team_members AS team_member
         WHERE team_member.team_id = team.id AND team_member.account_did = sqlc.arg(viewer_did)
       ), '')::text AS viewer_role
FROM core.organization_teams AS team
WHERE team.organization_id = sqlc.arg(organization_id)
  AND team.deleted_at IS NULL
  AND (
    sqlc.arg(include_secret)::boolean
    OR team.visibility = 'visible'
    OR EXISTS (
      SELECT 1 FROM core.organization_team_members AS viewer_membership
      WHERE viewer_membership.team_id = team.id
        AND viewer_membership.account_did = sqlc.arg(viewer_did)
    )
  )
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (lower(team.name), team.id) > (
      SELECT lower(cursor.name), cursor.id
      FROM core.organization_teams AS cursor
      WHERE cursor.organization_id = sqlc.arg(organization_id)
        AND cursor.id = sqlc.narg(after_id)::uuid
    )
  )
ORDER BY lower(team.name), team.id
LIMIT sqlc.arg(page_limit);

-- name: GetOrganizationTeam :one
SELECT * FROM core.organization_teams
WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL;

-- name: UpdateOrganizationTeam :one
WITH RECURSIVE descendants(descendant_id) AS (
  SELECT child.id
  FROM core.organization_teams AS child
  WHERE child.parent_team_id = sqlc.arg(id) AND child.deleted_at IS NULL
  UNION ALL
  SELECT child.id
  FROM core.organization_teams AS child
  JOIN descendants AS parent ON child.parent_team_id = parent.descendant_id
  WHERE child.deleted_at IS NULL
)
UPDATE core.organization_teams AS team
SET parent_team_id = sqlc.narg(parent_team_id),
    name = sqlc.arg(name),
    description = sqlc.narg(description),
    visibility = sqlc.arg(visibility),
    updated_at = sqlc.arg(updated_at)
WHERE team.id = sqlc.arg(id)
  AND team.organization_id = sqlc.arg(organization_id)
  AND team.deleted_at IS NULL
  AND (
    sqlc.narg(parent_team_id)::uuid IS NULL
    OR (
      sqlc.arg(visibility)::text = 'visible'
      AND sqlc.narg(parent_team_id)::uuid <> sqlc.arg(id)
      AND NOT EXISTS (SELECT 1 FROM descendants WHERE descendants.descendant_id = sqlc.narg(parent_team_id)::uuid)
      AND EXISTS (
        SELECT 1 FROM core.organization_teams AS parent
        WHERE parent.id = sqlc.narg(parent_team_id)::uuid
          AND parent.organization_id = sqlc.arg(organization_id)
          AND parent.visibility = 'visible'
          AND parent.deleted_at IS NULL
      )
    )
  )
  AND (
    sqlc.arg(visibility)::text <> 'secret'
    OR (
      sqlc.narg(parent_team_id)::uuid IS NULL
      AND NOT EXISTS (SELECT 1 FROM descendants)
    )
  )
RETURNING team.*;

-- name: OrganizationTeamHasChildren :one
SELECT EXISTS (
  SELECT 1 FROM core.organization_teams
  WHERE organization_id = sqlc.arg(organization_id)
    AND parent_team_id = sqlc.arg(team_id)
    AND deleted_at IS NULL
);

-- name: ListOrganizationTeamDescendantIDs :many
WITH RECURSIVE descendants(descendant_id) AS (
  SELECT child.id
  FROM core.organization_teams AS child
  WHERE child.organization_id = sqlc.arg(organization_id)
    AND child.parent_team_id = sqlc.arg(team_id)
    AND child.deleted_at IS NULL
  UNION ALL
  SELECT child.id
  FROM core.organization_teams AS child
  JOIN descendants AS parent ON child.parent_team_id = parent.descendant_id
  WHERE child.organization_id = sqlc.arg(organization_id)
    AND child.deleted_at IS NULL
)
SELECT descendant_id FROM descendants;

-- name: DeleteOrganizationTeamHierarchy :execrows
WITH RECURSIVE descendants AS (
  SELECT team.id
  FROM core.organization_teams AS team
  WHERE team.id = sqlc.arg(team_id)
    AND team.organization_id = sqlc.arg(organization_id)
    AND team.deleted_at IS NULL
  UNION ALL
  SELECT child.id
  FROM core.organization_teams AS child
  JOIN descendants AS parent ON child.parent_team_id = parent.id
  WHERE child.organization_id = sqlc.arg(organization_id)
    AND child.deleted_at IS NULL
)
UPDATE core.organization_teams
SET deleted_at = sqlc.arg(deleted_at), updated_at = sqlc.arg(deleted_at)
WHERE id IN (SELECT id FROM descendants);

-- name: AddOrganizationTeamMember :one
INSERT INTO core.organization_team_members (team_id, account_did, role, created_at, updated_at)
SELECT $1, $2, $3, $4, $4
WHERE EXISTS (
  SELECT 1 FROM core.organization_teams AS team
  JOIN core.organization_members AS member ON member.organization_id = team.organization_id
  WHERE team.id = $1 AND team.deleted_at IS NULL AND member.account_did = $2
)
ON CONFLICT (team_id, account_did) DO UPDATE SET role = EXCLUDED.role, updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: GetOrganizationTeamMember :one
SELECT * FROM core.organization_team_members
WHERE team_id = $1 AND account_did = $2;

-- name: ListOrganizationTeamMembers :many
SELECT team_member.*, account.handle_cache
FROM core.organization_team_members AS team_member
JOIN core.accounts AS account ON account.did = team_member.account_did
WHERE team_member.team_id = $1
ORDER BY CASE team_member.role WHEN 'maintainer' THEN 0 ELSE 1 END,
         lower(COALESCE(account.handle_cache, team_member.account_did));

-- name: PageOrganizationTeamMembers :many
SELECT team_member.*, account.handle_cache
FROM core.organization_team_members AS team_member
JOIN core.accounts AS account ON account.did = team_member.account_did
WHERE team_member.team_id = sqlc.arg(team_id)
  AND (sqlc.arg(after_did)::text = '' OR team_member.account_did > sqlc.arg(after_did)::text)
ORDER BY team_member.account_did
LIMIT sqlc.arg(page_limit);

-- name: DeleteOrganizationTeamMember :execrows
DELETE FROM core.organization_team_members WHERE team_id = $1 AND account_did = $2;

-- name: PutOrganizationTeamRepository :one
INSERT INTO core.organization_team_repositories (team_id, repository_id, role, created_at, updated_at)
SELECT sqlc.arg(team_id), sqlc.arg(repository_id), sqlc.arg(role), sqlc.arg(created_at), sqlc.arg(created_at)
FROM core.organization_teams AS team
JOIN core.repositories AS repository ON repository.organization_id = team.organization_id
WHERE team.id = sqlc.arg(team_id) AND repository.id = sqlc.arg(repository_id)
  AND team.deleted_at IS NULL AND repository.deleted_at IS NULL
ON CONFLICT (team_id, repository_id) DO UPDATE SET role = EXCLUDED.role, updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: ListOrganizationTeamRepositories :many
SELECT assignment.*, repository.slug AS repository_slug
FROM core.organization_team_repositories AS assignment
JOIN core.repositories AS repository ON repository.id = assignment.repository_id
WHERE assignment.team_id = $1 AND repository.deleted_at IS NULL
ORDER BY lower(repository.slug), repository.id;

-- name: PageOrganizationTeamRepositories :many
SELECT assignment.*, repository.slug AS repository_slug
FROM core.organization_team_repositories AS assignment
JOIN core.repositories AS repository ON repository.id = assignment.repository_id
WHERE assignment.team_id = sqlc.arg(team_id)
  AND repository.deleted_at IS NULL
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR assignment.repository_id > sqlc.narg(after_id)::uuid
  )
ORDER BY assignment.repository_id
LIMIT sqlc.arg(page_limit);

-- name: DeleteOrganizationTeamRepository :execrows
DELETE FROM core.organization_team_repositories WHERE team_id = $1 AND repository_id = $2;
