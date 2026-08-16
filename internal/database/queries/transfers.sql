-- name: ResolveRepositoryTransferOwner :one
SELECT route.alias, route.kind, route.account_did, route.organization_id,
       organization.creator_did, organization.at_uri AS organization_uri,
       organization.at_cid AS organization_cid
FROM core.owner_routes AS route
LEFT JOIN core.organizations AS organization
  ON organization.id = route.organization_id
 AND organization.state = 'active'
 AND organization.deleted_at IS NULL
WHERE lower(route.alias) = lower(sqlc.arg(owner))
   OR (route.kind = 'account' AND route.account_did = sqlc.arg(owner))
ORDER BY CASE WHEN lower(route.alias) = lower(sqlc.arg(owner)) THEN 0 ELSE 1 END, route.created_at DESC
LIMIT 1;

-- name: CanAcceptRepositoryTransfer :one
SELECT (CASE
    WHEN sqlc.narg(organization_id)::uuid IS NULL
      THEN sqlc.arg(account_did)::text = sqlc.arg(destination_owner_did)::text
    ELSE EXISTS (
      SELECT 1
      FROM core.organization_members AS member
      WHERE member.organization_id = sqlc.narg(organization_id)
        AND member.account_did = sqlc.arg(account_did)
        AND member.role = 'owner'
    )
  END)::boolean AS allowed;

-- name: CanInitiateRepositoryTransfer :one
SELECT EXISTS (
  SELECT 1
  FROM core.repositories AS repository
  LEFT JOIN core.organization_members AS member
    ON member.organization_id = repository.organization_id
   AND member.account_did = sqlc.arg(account_did)
  WHERE repository.id = sqlc.arg(repository_id)
    AND repository.state = 'active'
    AND repository.deleted_at IS NULL
    AND (
      (repository.organization_id IS NULL AND repository.owner_did = sqlc.arg(account_did))
      OR member.role = 'owner'
    )
) AS allowed;

-- name: ResolveRepositoryTransferSourceAlias :one
SELECT route.alias
FROM core.repositories AS repository
JOIN core.owner_routes AS route ON (
  (repository.organization_id IS NULL AND route.kind = 'account' AND route.account_did = repository.owner_did)
  OR (repository.organization_id IS NOT NULL AND route.kind = 'organization' AND route.organization_id = repository.organization_id)
)
WHERE repository.id = sqlc.arg(repository_id)
  AND repository.state = 'active'
  AND repository.deleted_at IS NULL
LIMIT 1;

-- name: GetRepositoryForTransfer :one
SELECT * FROM core.repositories
WHERE id = sqlc.arg(repository_id)
  AND state = 'active'
  AND deleted_at IS NULL;

-- name: CanCompleteRepositoryTransfer :one
SELECT EXISTS (
  SELECT 1
  FROM core.repository_transfers AS transfer
  JOIN core.repositories AS repository ON repository.id = transfer.repository_id
  WHERE transfer.id = sqlc.arg(id)
    AND transfer.status = 'pending'
    AND repository.state = 'active'
    AND repository.deleted_at IS NULL
    AND NOT EXISTS (
      SELECT 1
      FROM core.repositories AS conflict
      WHERE conflict.id <> repository.id
        AND conflict.deleted_at IS NULL
        AND lower(conflict.slug) = lower(repository.slug)
        AND (
          (transfer.destination_organization_id IS NULL
            AND conflict.organization_id IS NULL
            AND conflict.owner_did = transfer.destination_owner_did)
          OR conflict.organization_id = transfer.destination_organization_id
        )
    )
    AND NOT EXISTS (
      SELECT 1
      FROM core.repository_aliases AS conflict_alias
      WHERE conflict_alias.repository_id <> repository.id
        AND lower(conflict_alias.slug_alias) = lower(repository.slug)
        AND (
          lower(conflict_alias.owner_alias) IN (lower(transfer.destination_owner_alias), lower(transfer.source_owner_alias))
          OR (transfer.source_organization_id IS NULL AND lower(conflict_alias.owner_alias) = lower(transfer.source_owner_did))
        )
    )
)::boolean AS allowed;

-- name: GetPendingRepositoryTransfer :one
SELECT * FROM core.repository_transfers
WHERE repository_id = sqlc.arg(repository_id)
  AND status = 'pending';

-- name: CreateRepositoryTransfer :one
INSERT INTO core.repository_transfers (
    id, repository_id, source_owner_did, source_organization_id, source_owner_alias,
    source_repository_uri, source_repository_cid, destination_owner_did,
    destination_organization_id, destination_owner_alias, initiated_by_did,
    status, created_at, expires_at
)
VALUES (
    sqlc.arg(id), sqlc.arg(repository_id), sqlc.arg(source_owner_did),
    sqlc.narg(source_organization_id), sqlc.arg(source_owner_alias),
    sqlc.narg(source_repository_uri), sqlc.narg(source_repository_cid),
    sqlc.arg(destination_owner_did), sqlc.narg(destination_organization_id),
    sqlc.arg(destination_owner_alias), sqlc.arg(initiated_by_did), 'pending',
    sqlc.arg(created_at), sqlc.arg(expires_at)
)
RETURNING *;

-- name: GetRepositoryTransfer :one
SELECT * FROM core.repository_transfers WHERE id = sqlc.arg(id);

-- name: PageRepositoryTransfers :many
SELECT transfer.*
FROM core.repository_transfers AS transfer
WHERE transfer.repository_id = sqlc.arg(repository_id)
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (transfer.created_at, transfer.id) < (
      SELECT cursor.created_at, cursor.id
      FROM core.repository_transfers AS cursor
      WHERE cursor.repository_id = sqlc.arg(repository_id)
        AND cursor.id = sqlc.narg(after_id)::uuid
    )
  )
ORDER BY transfer.created_at DESC, transfer.id DESC
LIMIT sqlc.arg(page_limit);

-- name: SetRepositoryTransferProposal :one
UPDATE core.repository_transfers
SET proposal_uri = sqlc.arg(uri), proposal_cid = sqlc.arg(cid)
WHERE id = sqlc.arg(id)
  AND status = 'pending'
  AND (proposal_uri IS NULL OR proposal_uri = sqlc.arg(uri))
RETURNING *;

-- name: SetRepositoryTransferSuccessor :one
UPDATE core.repository_transfers
SET successor_uri = sqlc.arg(uri), successor_cid = sqlc.arg(cid)
WHERE id = sqlc.arg(id)
  AND status = 'pending'
  AND (successor_uri IS NULL OR successor_uri = sqlc.arg(uri))
RETURNING *;

-- name: StartRepositoryTransferAcceptance :one
UPDATE core.repository_transfers
SET acceptance_started_at = COALESCE(acceptance_started_at, sqlc.arg(started_at))
WHERE id = sqlc.arg(id)
  AND status = 'pending'
  AND (acceptance_started_at IS NOT NULL OR expires_at > sqlc.arg(started_at))
RETURNING *;

-- name: SetRepositoryTransferAcceptance :one
UPDATE core.repository_transfers
SET acceptance_uri = sqlc.arg(uri), acceptance_cid = sqlc.arg(cid)
WHERE id = sqlc.arg(id)
  AND status = 'pending'
  AND (acceptance_uri IS NULL OR acceptance_uri = sqlc.arg(uri))
RETURNING *;

-- name: SetRepositoryTransferSourceRedirect :one
UPDATE core.repository_transfers
SET source_redirect_cid = sqlc.arg(cid)
WHERE id = sqlc.arg(id)
  AND status = 'pending'
RETURNING *;

-- name: CancelRepositoryTransfer :one
UPDATE core.repository_transfers
SET status = 'cancelled', cancelled_at = sqlc.arg(cancelled_at)
WHERE id = sqlc.arg(id)
  AND status = 'pending'
  AND acceptance_started_at IS NULL
RETURNING *;

-- name: CompleteRepositoryTransfer :one
WITH selected AS (
  SELECT transfer.*
  FROM core.repository_transfers AS transfer
  WHERE transfer.id = sqlc.arg(id)
    AND transfer.status = 'pending'
    AND transfer.successor_uri IS NOT NULL
    AND transfer.acceptance_uri IS NOT NULL
    AND transfer.source_redirect_cid IS NOT NULL
    AND transfer.acceptance_started_at IS NOT NULL
    AND transfer.acceptance_started_at < transfer.expires_at
  FOR UPDATE
), route_available AS (
  SELECT selected.*
  FROM selected
  JOIN core.repositories AS repository ON repository.id = selected.repository_id
  WHERE NOT EXISTS (
    SELECT 1
    FROM core.repositories AS conflict
    WHERE conflict.id <> repository.id
      AND conflict.deleted_at IS NULL
      AND lower(conflict.slug) = lower(repository.slug)
      AND (
        (selected.destination_organization_id IS NULL
          AND conflict.organization_id IS NULL
          AND conflict.owner_did = selected.destination_owner_did)
        OR conflict.organization_id = selected.destination_organization_id
      )
  )
  AND NOT EXISTS (
    SELECT 1
    FROM core.repository_aliases AS conflict_alias
    WHERE conflict_alias.repository_id <> repository.id
      AND lower(conflict_alias.slug_alias) = lower(repository.slug)
      AND (
        lower(conflict_alias.owner_alias) IN (lower(selected.destination_owner_alias), lower(selected.source_owner_alias))
        OR (selected.source_organization_id IS NULL AND lower(conflict_alias.owner_alias) = lower(selected.source_owner_did))
      )
  )
), expected_aliases AS (
  SELECT sqlc.arg(alias_id)::uuid AS id, available.repository_id,
         available.source_owner_alias AS owner_alias, repository.slug AS slug_alias
  FROM route_available AS available
  JOIN core.repositories AS repository ON repository.id = available.repository_id
  UNION ALL
  SELECT sqlc.arg(source_did_alias_id)::uuid, available.repository_id,
         available.source_owner_did, repository.slug
  FROM route_available AS available
  JOIN core.repositories AS repository ON repository.id = available.repository_id
  WHERE available.source_organization_id IS NULL
    AND lower(available.source_owner_did) <> lower(available.source_owner_alias)
), alias AS (
  INSERT INTO core.repository_aliases (id, repository_id, owner_alias, slug_alias, created_at)
  SELECT expected.id, expected.repository_id, expected.owner_alias,
         expected.slug_alias, sqlc.arg(accepted_at)
  FROM expected_aliases AS expected
  ON CONFLICT (lower(owner_alias), lower(slug_alias)) DO UPDATE
    SET repository_id = EXCLUDED.repository_id
    WHERE core.repository_aliases.repository_id = EXCLUDED.repository_id
  RETURNING repository_id
), accepted_alias AS (
  SELECT alias.repository_id
  FROM alias
  GROUP BY alias.repository_id
  HAVING count(*) = (SELECT count(*) FROM expected_aliases)
), removed_teams AS (
  DELETE FROM core.organization_team_repositories AS team_repository
  USING accepted_alias
  WHERE team_repository.repository_id = accepted_alias.repository_id
), updated_repository AS (
  UPDATE core.repositories AS repository
  SET owner_did = available.destination_owner_did,
      organization_id = available.destination_organization_id,
      at_uri = available.successor_uri,
      at_cid = available.successor_cid,
      transferred_from_uri = available.source_repository_uri,
      transferred_from_cid = available.source_repository_cid,
      updated_at = sqlc.arg(accepted_at)
  FROM route_available AS available
  JOIN accepted_alias ON accepted_alias.repository_id = available.repository_id
  WHERE repository.id = available.repository_id
  RETURNING repository.id
), linked_projection AS (
  UPDATE network.repositories AS projection
  SET local_repository_id = updated_repository.id
  FROM updated_repository, route_available
  WHERE projection.uri = route_available.successor_uri
  RETURNING projection.uri
)
UPDATE core.repository_transfers AS transfer
SET status = 'completed', accepted_by_did = sqlc.arg(accepted_by_did), accepted_at = sqlc.arg(accepted_at)
FROM updated_repository
WHERE transfer.id = sqlc.arg(id)
  AND transfer.repository_id = updated_repository.id
RETURNING transfer.*;

-- name: CompletePrivateRepositoryTransfer :one
WITH selected AS (
  SELECT transfer.*
  FROM core.repository_transfers AS transfer
  WHERE transfer.id = sqlc.arg(id)
    AND transfer.status = 'pending'
    AND transfer.source_repository_uri IS NULL
    AND transfer.acceptance_started_at IS NOT NULL
    AND transfer.acceptance_started_at < transfer.expires_at
  FOR UPDATE
), route_available AS (
  SELECT selected.*
  FROM selected
  JOIN core.repositories AS repository ON repository.id = selected.repository_id
  WHERE NOT EXISTS (
    SELECT 1
    FROM core.repositories AS conflict
    WHERE conflict.id <> repository.id
      AND conflict.deleted_at IS NULL
      AND lower(conflict.slug) = lower(repository.slug)
      AND (
        (selected.destination_organization_id IS NULL
          AND conflict.organization_id IS NULL
          AND conflict.owner_did = selected.destination_owner_did)
        OR conflict.organization_id = selected.destination_organization_id
      )
  )
  AND NOT EXISTS (
    SELECT 1
    FROM core.repository_aliases AS conflict_alias
    WHERE conflict_alias.repository_id <> repository.id
      AND lower(conflict_alias.slug_alias) = lower(repository.slug)
      AND (
        lower(conflict_alias.owner_alias) IN (lower(selected.destination_owner_alias), lower(selected.source_owner_alias))
        OR (selected.source_organization_id IS NULL AND lower(conflict_alias.owner_alias) = lower(selected.source_owner_did))
      )
  )
), expected_aliases AS (
  SELECT sqlc.arg(alias_id)::uuid AS id, available.repository_id,
         available.source_owner_alias AS owner_alias, repository.slug AS slug_alias
  FROM route_available AS available
  JOIN core.repositories AS repository ON repository.id = available.repository_id
  UNION ALL
  SELECT sqlc.arg(source_did_alias_id)::uuid, available.repository_id,
         available.source_owner_did, repository.slug
  FROM route_available AS available
  JOIN core.repositories AS repository ON repository.id = available.repository_id
  WHERE available.source_organization_id IS NULL
    AND lower(available.source_owner_did) <> lower(available.source_owner_alias)
), alias AS (
  INSERT INTO core.repository_aliases (id, repository_id, owner_alias, slug_alias, created_at)
  SELECT expected.id, expected.repository_id, expected.owner_alias,
         expected.slug_alias, sqlc.arg(accepted_at)
  FROM expected_aliases AS expected
  ON CONFLICT (lower(owner_alias), lower(slug_alias)) DO UPDATE
    SET repository_id = EXCLUDED.repository_id
    WHERE core.repository_aliases.repository_id = EXCLUDED.repository_id
  RETURNING repository_id
), accepted_alias AS (
  SELECT alias.repository_id
  FROM alias
  GROUP BY alias.repository_id
  HAVING count(*) = (SELECT count(*) FROM expected_aliases)
), removed_teams AS (
  DELETE FROM core.organization_team_repositories AS team_repository
  USING accepted_alias
  WHERE team_repository.repository_id = accepted_alias.repository_id
), updated_repository AS (
  UPDATE core.repositories AS repository
  SET owner_did = available.destination_owner_did,
      organization_id = available.destination_organization_id,
      updated_at = sqlc.arg(accepted_at)
  FROM route_available AS available
  JOIN accepted_alias ON accepted_alias.repository_id = available.repository_id
  WHERE repository.id = available.repository_id
  RETURNING repository.id
)
UPDATE core.repository_transfers AS transfer
SET status = 'completed', accepted_by_did = sqlc.arg(accepted_by_did), accepted_at = sqlc.arg(accepted_at)
FROM updated_repository
WHERE transfer.id = sqlc.arg(id)
  AND transfer.repository_id = updated_repository.id
RETURNING transfer.*;
