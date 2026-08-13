-- name: ResolveOwner :one
WITH local_owner AS (
    SELECT route.kind,
           COALESCE(account.handle_cache, organization.slug, route.alias)::text AS canonical_name,
           route.account_did,
           organization.slug AS organization_slug,
           0::integer AS source_priority,
           route.created_at AS source_time
    FROM core.owner_routes AS route
    LEFT JOIN core.accounts AS account ON account.did = route.account_did
    LEFT JOIN core.organizations AS organization
      ON organization.id = route.organization_id AND organization.deleted_at IS NULL
    WHERE lower(route.alias) = lower(sqlc.arg(owner))
      AND (route.organization_id IS NULL OR organization.id IS NOT NULL)
), network_owner AS (
    SELECT 'account'::text AS kind,
           identity.handle::text AS canonical_name,
           identity.did::text AS account_did,
           NULL::text AS organization_slug,
           1::integer AS source_priority,
           identity.indexed_at AS source_time
    FROM network.identities AS identity
    WHERE lower(identity.handle) = lower(sqlc.arg(owner))
      AND identity.is_active
      AND identity.handle IS NOT NULL
)
SELECT kind, canonical_name, account_did, organization_slug
FROM (
    SELECT * FROM local_owner
    UNION ALL
    SELECT * FROM network_owner
) AS candidate
ORDER BY source_priority, source_time DESC, account_did
LIMIT 1;
