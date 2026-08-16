-- name: ResolveTriageRepository :one
WITH requested AS (
    SELECT requested_repository.canonical_uri
    FROM network.repositories AS requested_repository
    LEFT JOIN network.profiles AS requested_profile
      ON requested_profile.did = requested_repository.owner_did AND requested_profile.deleted_at IS NULL
    LEFT JOIN network.identities AS requested_identity
      ON requested_identity.did = requested_repository.owner_did AND requested_identity.is_active
    LEFT JOIN network.organizations AS requested_organization
      ON requested_organization.uri = requested_repository.organization_uri AND requested_organization.deleted_at IS NULL
    WHERE requested_repository.deleted_at IS NULL AND requested_repository.cid IS NOT NULL
      AND lower(requested_repository.slug) = lower(sqlc.arg(repository_slug)::text)
      AND (
          requested_repository.owner_did = sqlc.arg(repository_owner)::text
          OR lower(coalesce(requested_profile.handle, requested_identity.handle, '')) = lower(sqlc.arg(repository_owner)::text)
          OR lower(coalesce(requested_organization.slug, '')) = lower(sqlc.arg(repository_owner)::text)
      )
    ORDER BY requested_repository.indexed_at DESC, requested_repository.uri DESC
    LIMIT 1
)
SELECT local_repository.id, repository.uri, repository.cid, repository.owner_did
FROM requested
JOIN network.repositories AS repository ON repository.uri = requested.canonical_uri
JOIN core.repositories AS local_repository ON local_repository.id = repository.local_repository_id
WHERE repository.deleted_at IS NULL AND repository.cid IS NOT NULL
  AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL;

-- name: ResolveReadableTriageRepository :one
SELECT repository.uri, repository.cid, repository.owner_did
FROM network.repositories AS repository
LEFT JOIN network.profiles AS profile
  ON profile.did = repository.owner_did AND profile.deleted_at IS NULL
LEFT JOIN network.identities AS identity
  ON identity.did = repository.owner_did AND identity.is_active
LEFT JOIN network.organizations AS organization
  ON organization.uri = repository.organization_uri AND organization.deleted_at IS NULL
WHERE repository.deleted_at IS NULL AND repository.cid IS NOT NULL
  AND lower(repository.slug) = lower(sqlc.arg(repository_slug)::text)
  AND (
      repository.owner_did = sqlc.arg(repository_owner)::text
      OR lower(coalesce(profile.handle, identity.handle, '')) = lower(sqlc.arg(repository_owner)::text)
      OR lower(coalesce(organization.slug, '')) = lower(sqlc.arg(repository_owner)::text)
  )
ORDER BY (repository.uri = repository.canonical_uri) DESC, repository.indexed_at DESC, repository.uri DESC
LIMIT 1;

-- name: ListRepositoryLabels :many
SELECT label.*
FROM network.repository_labels AS label
JOIN network.repositories AS label_repository ON label_repository.uri = label.repository_uri
JOIN network.repositories AS requested_repository ON requested_repository.uri = sqlc.arg(repository_uri)::text
WHERE label_repository.lineage_uri = requested_repository.lineage_uri
  AND label.author_did = label_repository.owner_did
  AND label.deleted_at IS NULL AND label.cid IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM moderation.blocked_dids AS block
      WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = label.author_did
  )
  AND NOT EXISTS (
      SELECT 1 FROM moderation.hidden_records AS hidden
      WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = label.uri
  )
  AND (sqlc.narg(cursor_uri)::text IS NULL OR label.uri < sqlc.narg(cursor_uri)::text)
ORDER BY label.uri DESC
LIMIT sqlc.arg(result_limit);

-- name: GetRepositoryLabel :one
SELECT label.*
FROM network.repository_labels AS label
JOIN network.repositories AS label_repository ON label_repository.uri = label.repository_uri
JOIN network.repositories AS requested_repository ON requested_repository.uri = sqlc.arg(repository_uri)::text
WHERE label_repository.lineage_uri = requested_repository.lineage_uri
  AND label.author_did = label_repository.owner_did
  AND label.rkey = sqlc.arg(label_id)::text
  AND label.deleted_at IS NULL AND label.cid IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM moderation.blocked_dids AS block
      WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = label.author_did
  )
  AND NOT EXISTS (
      SELECT 1 FROM moderation.hidden_records AS hidden
      WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = label.uri
  )
ORDER BY (label.repository_uri = requested_repository.canonical_uri) DESC,
         label.source_event_id DESC, label.uri DESC
LIMIT 1;

-- name: ListRepositoryMilestones :many
SELECT milestone.*
FROM network.repository_milestones AS milestone
JOIN network.repositories AS milestone_repository ON milestone_repository.uri = milestone.repository_uri
JOIN network.repositories AS requested_repository ON requested_repository.uri = sqlc.arg(repository_uri)::text
WHERE milestone_repository.lineage_uri = requested_repository.lineage_uri
  AND milestone.author_did = milestone_repository.owner_did
  AND milestone.deleted_at IS NULL AND milestone.cid IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM moderation.blocked_dids AS block
      WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = milestone.author_did
  )
  AND NOT EXISTS (
      SELECT 1 FROM moderation.hidden_records AS hidden
      WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = milestone.uri
  )
  AND (sqlc.narg(cursor_uri)::text IS NULL OR milestone.uri < sqlc.narg(cursor_uri)::text)
ORDER BY milestone.uri DESC
LIMIT sqlc.arg(result_limit);

-- name: GetRepositoryMilestone :one
SELECT milestone.*
FROM network.repository_milestones AS milestone
JOIN network.repositories AS milestone_repository ON milestone_repository.uri = milestone.repository_uri
JOIN network.repositories AS requested_repository ON requested_repository.uri = sqlc.arg(repository_uri)::text
WHERE milestone_repository.lineage_uri = requested_repository.lineage_uri
  AND milestone.author_did = milestone_repository.owner_did
  AND milestone.rkey = sqlc.arg(milestone_id)::text
  AND milestone.deleted_at IS NULL AND milestone.cid IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM moderation.blocked_dids AS block
      WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = milestone.author_did
  )
  AND NOT EXISTS (
      SELECT 1 FROM moderation.hidden_records AS hidden
      WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = milestone.uri
  )
ORDER BY (milestone.repository_uri = requested_repository.canonical_uri) DESC,
         milestone.source_event_id DESC, milestone.uri DESC
LIMIT 1;

-- name: ResolveIssueTriageSubject :one
WITH target AS (
    SELECT * FROM network.repositories WHERE uri = sqlc.arg(repository_uri)::text
)
SELECT issue.uri, issue.cid
FROM network.issues AS issue
JOIN network.repositories AS observed_repository ON observed_repository.uri = issue.repository_uri
JOIN target ON target.lineage_uri = observed_repository.lineage_uri
WHERE issue.uri = sqlc.arg(subject_uri)::text
  AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL;

-- name: ResolvePullRequestTriageSubject :one
WITH target AS (
    SELECT * FROM network.repositories WHERE uri = sqlc.arg(repository_uri)::text
)
SELECT pull_request.uri, pull_request.cid
FROM network.pull_requests AS pull_request
JOIN network.repositories AS observed_repository ON observed_repository.uri = pull_request.target_repository_uri
JOIN target ON target.lineage_uri = observed_repository.lineage_uri
WHERE pull_request.uri = sqlc.arg(subject_uri)::text
  AND pull_request.deleted_at IS NULL AND pull_request.cid IS NOT NULL;

-- name: GetSubjectTriage :one
SELECT metadata.*
FROM network.subject_triage AS metadata
JOIN network.repositories AS metadata_repository ON metadata_repository.uri = metadata.repository_uri
JOIN network.repositories AS requested_repository ON requested_repository.uri = sqlc.arg(repository_uri)::text
WHERE metadata.subject_uri = sqlc.arg(subject_uri)::text
  AND metadata.subject_kind = sqlc.arg(subject_kind)::text
  AND metadata_repository.lineage_uri = requested_repository.lineage_uri
  AND metadata.author_did = metadata_repository.owner_did
  AND metadata.deleted_at IS NULL AND metadata.cid IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM moderation.blocked_dids AS block
      WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = metadata.author_did
  )
  AND NOT EXISTS (
      SELECT 1 FROM moderation.hidden_records AS hidden
      WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = metadata.uri
  )
ORDER BY metadata.source_event_id DESC, metadata.uri DESC
LIMIT 1;

-- name: ListSubjectTriageLabels :many
SELECT label.*
FROM network.subject_triage AS metadata
CROSS JOIN LATERAL unnest(metadata.label_uris) WITH ORDINALITY AS selected(uri, position)
JOIN network.repository_labels AS label ON label.uri = selected.uri
JOIN network.repositories AS label_repository ON label_repository.uri = label.repository_uri
JOIN network.repositories AS metadata_repository ON metadata_repository.uri = metadata.repository_uri
WHERE metadata.uri = sqlc.arg(metadata_uri)::text
  AND label_repository.lineage_uri = metadata_repository.lineage_uri
  AND label.author_did = label_repository.owner_did
  AND label.deleted_at IS NULL AND label.cid IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM moderation.blocked_dids AS block
      WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = label.author_did
  )
  AND NOT EXISTS (
      SELECT 1 FROM moderation.hidden_records AS hidden
      WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = label.uri
  )
ORDER BY selected.position;

-- name: ListSubjectTriageAssignees :many
SELECT profile.did, coalesce(identity.handle, profile.handle) AS handle, profile.display_name
FROM network.subject_triage AS metadata
CROSS JOIN LATERAL unnest(metadata.assignee_dids) WITH ORDINALITY AS selected(did, position)
JOIN network.profiles AS profile ON profile.did = selected.did
LEFT JOIN network.identities AS identity ON identity.did = profile.did AND identity.is_active
WHERE metadata.uri = sqlc.arg(metadata_uri)::text
  AND profile.deleted_at IS NULL
  AND (identity.did IS NULL OR identity.is_active)
  AND NOT EXISTS (
      SELECT 1 FROM moderation.blocked_dids AS block
      WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = profile.did
  )
  AND NOT EXISTS (
      SELECT 1 FROM moderation.hidden_records AS hidden
      WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = profile.profile_uri
  )
ORDER BY selected.position;

-- name: GetSubjectTriageMilestone :one
SELECT milestone.*
FROM network.subject_triage AS metadata
JOIN network.repository_milestones AS milestone ON milestone.uri = metadata.milestone_uri
JOIN network.repositories AS milestone_repository ON milestone_repository.uri = milestone.repository_uri
JOIN network.repositories AS metadata_repository ON metadata_repository.uri = metadata.repository_uri
WHERE metadata.uri = sqlc.arg(metadata_uri)::text
  AND milestone_repository.lineage_uri = metadata_repository.lineage_uri
  AND milestone.author_did = milestone_repository.owner_did
  AND milestone.deleted_at IS NULL AND milestone.cid IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM moderation.blocked_dids AS block
      WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = milestone.author_did
  )
  AND NOT EXISTS (
      SELECT 1 FROM moderation.hidden_records AS hidden
      WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = milestone.uri
  );

-- name: ResolveRepositoryLabelURIs :many
SELECT label.rkey, label.uri
FROM network.repository_labels AS label
JOIN network.repositories AS label_repository ON label_repository.uri = label.repository_uri
JOIN network.repositories AS requested_repository ON requested_repository.uri = sqlc.arg(repository_uri)::text
WHERE label.rkey = ANY(sqlc.arg(label_ids)::text[])
  AND label_repository.lineage_uri = requested_repository.lineage_uri
  AND label.author_did = label_repository.owner_did
  AND label.deleted_at IS NULL AND label.cid IS NOT NULL
ORDER BY label.rkey, label.source_event_id DESC;

-- name: ResolveRepositoryMilestoneURI :one
SELECT milestone.uri
FROM network.repository_milestones AS milestone
JOIN network.repositories AS milestone_repository ON milestone_repository.uri = milestone.repository_uri
JOIN network.repositories AS requested_repository ON requested_repository.uri = sqlc.arg(repository_uri)::text
WHERE milestone.rkey = sqlc.arg(milestone_id)::text
  AND milestone_repository.lineage_uri = requested_repository.lineage_uri
  AND milestone.author_did = milestone_repository.owner_did
  AND milestone.deleted_at IS NULL AND milestone.cid IS NOT NULL
ORDER BY milestone.source_event_id DESC
LIMIT 1;

-- name: CountVisibleTriageAssignees :one
SELECT count(*)
FROM network.profiles AS profile
LEFT JOIN network.identities AS identity ON identity.did = profile.did
WHERE profile.did = ANY(sqlc.arg(assignee_dids)::text[])
  AND profile.deleted_at IS NULL
  AND (identity.did IS NULL OR identity.is_active);

-- name: UpsertFederationRepositoryLabel :exec
INSERT INTO network.repository_labels (
    uri, cid, author_did, rkey, repository_uri, repository_cid, name, color,
    description, record_created_at, record_updated_at, indexed_at, deleted_at,
    source_event_id
)
SELECT sqlc.arg(uri), sqlc.arg(cid), sqlc.arg(author_did), sqlc.arg(rkey),
       sqlc.arg(repository_uri), sqlc.arg(repository_cid), sqlc.arg(name),
       sqlc.arg(color), sqlc.arg(description), sqlc.arg(record_created_at),
       sqlc.arg(record_updated_at), sqlc.arg(indexed_at), NULL, sqlc.arg(source_event_id)
FROM network.records AS source_record
WHERE source_record.uri = sqlc.arg(uri)
  AND source_record.source_event_id = sqlc.arg(source_event_id)
  AND source_record.deleted_at IS NULL
ON CONFLICT (uri) DO UPDATE SET
    cid = EXCLUDED.cid,
    repository_cid = EXCLUDED.repository_cid,
    name = EXCLUDED.name,
    color = EXCLUDED.color,
    description = EXCLUDED.description,
    record_created_at = EXCLUDED.record_created_at,
    record_updated_at = EXCLUDED.record_updated_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.repository_labels.source_event_id < EXCLUDED.source_event_id
  AND network.repository_labels.author_did = EXCLUDED.author_did
  AND network.repository_labels.rkey = EXCLUDED.rkey
  AND network.repository_labels.repository_uri = EXCLUDED.repository_uri
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = EXCLUDED.uri
        AND current_record.source_event_id = EXCLUDED.source_event_id
        AND current_record.deleted_at IS NULL
  );

-- name: TombstoneFederationRepositoryLabel :exec
UPDATE network.repository_labels AS label SET
    cid = NULL,
    indexed_at = sqlc.arg(indexed_at),
    deleted_at = sqlc.arg(indexed_at),
    source_event_id = sqlc.arg(source_event_id)
WHERE label.uri = sqlc.arg(uri)
  AND label.source_event_id < sqlc.arg(source_event_id)
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = sqlc.arg(uri)
        AND current_record.source_event_id = sqlc.arg(source_event_id)
        AND current_record.deleted_at IS NOT NULL
  );

-- name: UpsertFederationRepositoryMilestone :exec
INSERT INTO network.repository_milestones (
    uri, cid, author_did, rkey, repository_uri, repository_cid, title,
    description, state, due_at, closed_at, record_created_at, record_updated_at,
    indexed_at, deleted_at, source_event_id
)
SELECT sqlc.arg(uri), sqlc.arg(cid), sqlc.arg(author_did), sqlc.arg(rkey),
       sqlc.arg(repository_uri), sqlc.arg(repository_cid), sqlc.arg(title),
       sqlc.arg(description), sqlc.arg(state), sqlc.narg(due_at), sqlc.narg(closed_at),
       sqlc.arg(record_created_at), sqlc.arg(record_updated_at), sqlc.arg(indexed_at),
       NULL, sqlc.arg(source_event_id)
FROM network.records AS source_record
WHERE source_record.uri = sqlc.arg(uri)
  AND source_record.source_event_id = sqlc.arg(source_event_id)
  AND source_record.deleted_at IS NULL
ON CONFLICT (uri) DO UPDATE SET
    cid = EXCLUDED.cid,
    repository_cid = EXCLUDED.repository_cid,
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    state = EXCLUDED.state,
    due_at = EXCLUDED.due_at,
    closed_at = EXCLUDED.closed_at,
    record_created_at = EXCLUDED.record_created_at,
    record_updated_at = EXCLUDED.record_updated_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.repository_milestones.source_event_id < EXCLUDED.source_event_id
  AND network.repository_milestones.author_did = EXCLUDED.author_did
  AND network.repository_milestones.rkey = EXCLUDED.rkey
  AND network.repository_milestones.repository_uri = EXCLUDED.repository_uri
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = EXCLUDED.uri
        AND current_record.source_event_id = EXCLUDED.source_event_id
        AND current_record.deleted_at IS NULL
  );

-- name: TombstoneFederationRepositoryMilestone :exec
UPDATE network.repository_milestones AS milestone SET
    cid = NULL,
    indexed_at = sqlc.arg(indexed_at),
    deleted_at = sqlc.arg(indexed_at),
    source_event_id = sqlc.arg(source_event_id)
WHERE milestone.uri = sqlc.arg(uri)
  AND milestone.source_event_id < sqlc.arg(source_event_id)
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = sqlc.arg(uri)
        AND current_record.source_event_id = sqlc.arg(source_event_id)
        AND current_record.deleted_at IS NOT NULL
  );

-- name: UpsertFederationSubjectTriage :exec
INSERT INTO network.subject_triage (
    uri, cid, author_did, rkey, subject_uri, subject_cid, subject_kind,
    repository_uri, repository_cid, label_uris, assignee_dids, milestone_uri,
    record_created_at, record_updated_at, indexed_at, deleted_at, source_event_id
)
SELECT sqlc.arg(uri), sqlc.arg(cid), sqlc.arg(author_did), sqlc.arg(rkey),
       sqlc.arg(subject_uri), sqlc.arg(subject_cid), sqlc.arg(subject_kind),
       sqlc.arg(repository_uri), sqlc.arg(repository_cid), sqlc.arg(label_uris),
       sqlc.arg(assignee_dids), sqlc.narg(milestone_uri), sqlc.arg(record_created_at),
       sqlc.arg(record_updated_at), sqlc.arg(indexed_at), NULL, sqlc.arg(source_event_id)
FROM network.records AS source_record
WHERE source_record.uri = sqlc.arg(uri)
  AND source_record.source_event_id = sqlc.arg(source_event_id)
  AND source_record.deleted_at IS NULL
ON CONFLICT (uri) DO UPDATE SET
    cid = EXCLUDED.cid,
    subject_cid = EXCLUDED.subject_cid,
    repository_cid = EXCLUDED.repository_cid,
    label_uris = EXCLUDED.label_uris,
    assignee_dids = EXCLUDED.assignee_dids,
    milestone_uri = EXCLUDED.milestone_uri,
    record_created_at = EXCLUDED.record_created_at,
    record_updated_at = EXCLUDED.record_updated_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.subject_triage.source_event_id < EXCLUDED.source_event_id
  AND network.subject_triage.author_did = EXCLUDED.author_did
  AND network.subject_triage.rkey = EXCLUDED.rkey
  AND network.subject_triage.subject_uri = EXCLUDED.subject_uri
  AND network.subject_triage.subject_kind = EXCLUDED.subject_kind
  AND network.subject_triage.repository_uri = EXCLUDED.repository_uri
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = EXCLUDED.uri
        AND current_record.source_event_id = EXCLUDED.source_event_id
        AND current_record.deleted_at IS NULL
  );

-- name: TombstoneFederationSubjectTriage :exec
UPDATE network.subject_triage AS metadata SET
    cid = NULL,
    indexed_at = sqlc.arg(indexed_at),
    deleted_at = sqlc.arg(indexed_at),
    source_event_id = sqlc.arg(source_event_id)
WHERE metadata.uri = sqlc.arg(uri)
  AND metadata.source_event_id < sqlc.arg(source_event_id)
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = sqlc.arg(uri)
        AND current_record.source_event_id = sqlc.arg(source_event_id)
        AND current_record.deleted_at IS NOT NULL
  );
