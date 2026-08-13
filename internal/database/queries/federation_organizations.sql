-- name: UpsertFederationOrganization :exec
INSERT INTO network.organizations (uri, cid, creator_did, rkey, slug, name, description, website, location, record_created_at, record_updated_at, indexed_at, source_event_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (uri) DO UPDATE SET cid=EXCLUDED.cid, slug=EXCLUDED.slug, name=EXCLUDED.name, description=EXCLUDED.description, website=EXCLUDED.website, location=EXCLUDED.location, record_created_at=EXCLUDED.record_created_at, record_updated_at=EXCLUDED.record_updated_at, indexed_at=EXCLUDED.indexed_at, source_event_id=EXCLUDED.source_event_id, deleted_at=NULL
WHERE network.organizations.source_event_id < EXCLUDED.source_event_id;

-- name: UpsertFederationOrganizationGrant :exec
INSERT INTO network.organization_grants (uri,cid,author_did,rkey,organization_uri,organization_cid,subject_did,role,authority_uri,authority_cid,record_created_at,expires_at,indexed_at,source_event_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (uri) DO UPDATE SET cid=EXCLUDED.cid,organization_uri=EXCLUDED.organization_uri,organization_cid=EXCLUDED.organization_cid,subject_did=EXCLUDED.subject_did,role=EXCLUDED.role,authority_uri=EXCLUDED.authority_uri,authority_cid=EXCLUDED.authority_cid,record_created_at=EXCLUDED.record_created_at,expires_at=EXCLUDED.expires_at,indexed_at=EXCLUDED.indexed_at,source_event_id=EXCLUDED.source_event_id,deleted_at=NULL
WHERE network.organization_grants.source_event_id < EXCLUDED.source_event_id;

-- name: UpsertFederationOrganizationMembership :exec
INSERT INTO network.organization_memberships (uri,cid,author_did,rkey,organization_uri,organization_cid,grant_uri,grant_cid,visibility,record_created_at,record_updated_at,indexed_at,source_event_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (uri) DO UPDATE SET cid=EXCLUDED.cid,organization_uri=EXCLUDED.organization_uri,organization_cid=EXCLUDED.organization_cid,grant_uri=EXCLUDED.grant_uri,grant_cid=EXCLUDED.grant_cid,visibility=EXCLUDED.visibility,record_created_at=EXCLUDED.record_created_at,record_updated_at=EXCLUDED.record_updated_at,indexed_at=EXCLUDED.indexed_at,source_event_id=EXCLUDED.source_event_id,deleted_at=NULL
WHERE network.organization_memberships.source_event_id < EXCLUDED.source_event_id;

-- name: UpsertFederationOrganizationRevocation :exec
INSERT INTO network.organization_revocations (uri,cid,author_did,rkey,organization_uri,organization_cid,grant_uri,grant_cid,subject_did,authority_uri,authority_cid,record_created_at,indexed_at,source_event_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (uri) DO UPDATE SET cid=EXCLUDED.cid,organization_uri=EXCLUDED.organization_uri,organization_cid=EXCLUDED.organization_cid,grant_uri=EXCLUDED.grant_uri,grant_cid=EXCLUDED.grant_cid,subject_did=EXCLUDED.subject_did,authority_uri=EXCLUDED.authority_uri,authority_cid=EXCLUDED.authority_cid,record_created_at=EXCLUDED.record_created_at,indexed_at=EXCLUDED.indexed_at,source_event_id=EXCLUDED.source_event_id,deleted_at=NULL
WHERE network.organization_revocations.source_event_id < EXCLUDED.source_event_id;

-- name: TombstoneFederationOrganizationProjection :exec
UPDATE network.organizations SET deleted_at=$2,indexed_at=$2,source_event_id=$3 WHERE uri=$1 AND source_event_id < $3;
-- name: TombstoneFederationOrganizationGrant :exec
UPDATE network.organization_grants SET deleted_at=$2,indexed_at=$2,source_event_id=$3 WHERE uri=$1 AND source_event_id < $3;
-- name: TombstoneFederationOrganizationMembership :exec
UPDATE network.organization_memberships SET deleted_at=$2,indexed_at=$2,source_event_id=$3 WHERE uri=$1 AND source_event_id < $3;
-- name: TombstoneFederationOrganizationRevocation :exec
UPDATE network.organization_revocations SET deleted_at=$2,indexed_at=$2,source_event_id=$3 WHERE uri=$1 AND source_event_id < $3;
