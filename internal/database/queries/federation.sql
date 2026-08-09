-- name: HasFederationReceipt :one
SELECT EXISTS (
    SELECT 1 FROM ops.federation_receipts WHERE consumer = $1 AND event_id = $2
);

-- name: InsertFederationReceipt :exec
INSERT INTO ops.federation_receipts (consumer, event_id, outcome, rejection, received_at)
VALUES ($1, $2, $3, $4, $5);

-- name: AdvanceFederationCursor :exec
INSERT INTO ops.federation_cursors (consumer, event_id, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (consumer) DO UPDATE SET
    event_id = GREATEST(ops.federation_cursors.event_id, EXCLUDED.event_id),
    updated_at = CASE
        WHEN EXCLUDED.event_id > ops.federation_cursors.event_id THEN EXCLUDED.updated_at
        ELSE ops.federation_cursors.updated_at
    END;

-- name: UpsertFederationRecord :exec
INSERT INTO network.records (
    uri, cid, author_did, collection, rkey, record, record_created_at,
    indexed_at, deleted_at, source_event_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, $9)
ON CONFLICT (uri) DO UPDATE SET
    cid = EXCLUDED.cid,
    author_did = EXCLUDED.author_did,
    collection = EXCLUDED.collection,
    rkey = EXCLUDED.rkey,
    record = EXCLUDED.record,
    record_created_at = EXCLUDED.record_created_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.records.source_event_id < EXCLUDED.source_event_id;

-- name: TombstoneFederationRecord :exec
INSERT INTO network.records (
    uri, author_did, collection, rkey, indexed_at, deleted_at, source_event_id
) VALUES ($1, $2, $3, $4, $5, $5, $6)
ON CONFLICT (uri) DO UPDATE SET
    cid = NULL,
    record = NULL,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = EXCLUDED.deleted_at,
    source_event_id = EXCLUDED.source_event_id
WHERE network.records.source_event_id < EXCLUDED.source_event_id;

-- name: UpsertFederationIdentity :exec
INSERT INTO network.identities (did, handle, status, is_active, indexed_at, source_event_id)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (did) DO UPDATE SET
    handle = EXCLUDED.handle,
    status = EXCLUDED.status,
    is_active = EXCLUDED.is_active,
    indexed_at = EXCLUDED.indexed_at,
    source_event_id = EXCLUDED.source_event_id
WHERE network.identities.source_event_id < EXCLUDED.source_event_id;

-- name: ProjectIdentityHandle :exec
UPDATE network.profiles SET handle = $2, indexed_at = $3, handle_source_event_id = $4
WHERE did = $1 AND (handle_source_event_id IS NULL OR handle_source_event_id < $4);

-- name: UpsertFederationProfile :exec
INSERT INTO network.profiles (
    did, profile_uri, profile_cid, handle, display_name, bio, website, location,
    record_created_at, indexed_at, deleted_at, source_event_id, handle_source_event_id
) VALUES (
    $1, $2, $3, (SELECT handle FROM network.identities WHERE did = $1),
    $4, $5, $6, $7, $8, $9, NULL, $10,
    (SELECT source_event_id FROM network.identities WHERE did = $1)
)
ON CONFLICT (did) DO UPDATE SET
    profile_uri = EXCLUDED.profile_uri,
    profile_cid = EXCLUDED.profile_cid,
    handle = COALESCE(EXCLUDED.handle, network.profiles.handle),
	handle_source_event_id = COALESCE(EXCLUDED.handle_source_event_id, network.profiles.handle_source_event_id),
    display_name = EXCLUDED.display_name,
    bio = EXCLUDED.bio,
    website = EXCLUDED.website,
    location = EXCLUDED.location,
    record_created_at = EXCLUDED.record_created_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.profiles.source_event_id IS NULL OR network.profiles.source_event_id < EXCLUDED.source_event_id;

-- name: TombstoneFederationProfile :exec
INSERT INTO network.profiles (did, profile_uri, indexed_at, deleted_at, source_event_id)
VALUES ($1, $2, $3, $3, $4)
ON CONFLICT (did) DO UPDATE SET
    profile_cid = NULL,
    display_name = NULL,
    bio = NULL,
    website = NULL,
    location = NULL,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = EXCLUDED.deleted_at,
    source_event_id = EXCLUDED.source_event_id
WHERE network.profiles.source_event_id IS NULL OR network.profiles.source_event_id < EXCLUDED.source_event_id;

-- name: UpsertFederationRepository :exec
INSERT INTO network.repositories (
    uri, cid, owner_did, rkey, slug, name, description, default_branch,
    git_https, git_ssh, web, local_repository_id, record_created_at,
    record_updated_at, indexed_at, deleted_at, source_event_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
    (SELECT id FROM core.repositories WHERE at_uri = $1), $12, $13, $14, NULL, $15
)
ON CONFLICT (uri) DO UPDATE SET
    cid = EXCLUDED.cid,
    owner_did = EXCLUDED.owner_did,
    rkey = EXCLUDED.rkey,
    slug = EXCLUDED.slug,
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    default_branch = EXCLUDED.default_branch,
    git_https = EXCLUDED.git_https,
    git_ssh = EXCLUDED.git_ssh,
    web = EXCLUDED.web,
    local_repository_id = EXCLUDED.local_repository_id,
    record_created_at = EXCLUDED.record_created_at,
    record_updated_at = EXCLUDED.record_updated_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.repositories.source_event_id < EXCLUDED.source_event_id;

-- name: TombstoneFederationRepository :exec
INSERT INTO network.repositories (uri, owner_did, rkey, indexed_at, deleted_at, source_event_id)
VALUES ($1, $2, $3, $4, $4, $5)
ON CONFLICT (uri) DO UPDATE SET
    cid = NULL,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = EXCLUDED.deleted_at,
    source_event_id = EXCLUDED.source_event_id
WHERE network.repositories.source_event_id < EXCLUDED.source_event_id;

-- name: RecomputeFederationRepositoryCount :exec
UPDATE network.profiles SET repository_count = (
    SELECT count(*) FROM network.repositories
    WHERE owner_did = $1 AND deleted_at IS NULL
)
WHERE did = $1;

-- name: UpsertFederationStar :one
INSERT INTO network.stars (
    uri, cid, author_did, rkey, repository_uri, repository_cid,
    record_created_at, indexed_at, deleted_at, source_event_id
)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, NULL, $9
FROM network.records AS source_record
WHERE source_record.uri = $1
  AND source_record.source_event_id = $9
  AND source_record.deleted_at IS NULL
ON CONFLICT (uri) DO UPDATE SET
    cid = EXCLUDED.cid,
    author_did = EXCLUDED.author_did,
    rkey = EXCLUDED.rkey,
    repository_uri = EXCLUDED.repository_uri,
    repository_cid = EXCLUDED.repository_cid,
    record_created_at = EXCLUDED.record_created_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.stars.source_event_id < EXCLUDED.source_event_id
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = EXCLUDED.uri
        AND current_record.source_event_id = EXCLUDED.source_event_id
        AND current_record.deleted_at IS NULL
  )
RETURNING repository_uri;

-- name: TombstoneFederationStar :one
UPDATE network.stars AS star SET
    cid = NULL,
    indexed_at = $2,
    deleted_at = $2,
    source_event_id = $3
WHERE star.uri = $1
  AND star.source_event_id < $3
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = $1
        AND current_record.source_event_id = $3
        AND current_record.deleted_at IS NOT NULL
  )
RETURNING repository_uri;

-- name: RecomputeFederationStarCount :exec
UPDATE network.repositories SET star_count = (
    SELECT count(*) FROM network.stars
    WHERE repository_uri = $1 AND deleted_at IS NULL
)
WHERE uri = $1;

-- name: LockFederationRepositoryStars :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(repository_uri), 1869636979));

-- name: GetFederationStarRepositoryURI :one
SELECT repository_uri
FROM network.stars
WHERE uri = $1;

-- name: GetFederationIssueRepositoryURI :one
SELECT repository_uri
FROM network.issues
WHERE uri = $1;

-- name: GetFederationIssueStatusTarget :one
SELECT issue_uri, repository_uri
FROM network.issue_statuses
WHERE uri = $1;

-- name: GetFederationIssueCommentIssueURI :one
SELECT issue_uri
FROM network.issue_comments
WHERE uri = $1;

-- name: ListFederationCommentChildIssueURIs :many
SELECT DISTINCT issue_uri
FROM network.issue_comments
WHERE parent_uri = $1
  AND deleted_at IS NULL;

-- name: GetFederationIssueRepositoryForComment :one
SELECT repository_uri
FROM network.issues
WHERE uri = $1;

-- name: LockFederationIssueComments :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(issue_uri), 1769236596));

-- name: LockFederationRepositoryIssues :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(repository_uri), 1769173093));

-- name: UpsertFederationIssue :one
INSERT INTO network.issues (
    uri, cid, author_did, rkey, repository_uri, repository_cid, title, body,
    record_created_at, record_updated_at, indexed_at, deleted_at, source_event_id
)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULL, $12
FROM network.records AS source_record
WHERE source_record.uri = $1
  AND source_record.source_event_id = $12
  AND source_record.deleted_at IS NULL
ON CONFLICT (uri) DO UPDATE SET
    cid = EXCLUDED.cid,
    author_did = EXCLUDED.author_did,
    rkey = EXCLUDED.rkey,
    repository_uri = EXCLUDED.repository_uri,
    repository_cid = EXCLUDED.repository_cid,
    title = EXCLUDED.title,
    body = EXCLUDED.body,
    record_created_at = EXCLUDED.record_created_at,
    record_updated_at = EXCLUDED.record_updated_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.issues.source_event_id < EXCLUDED.source_event_id
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = EXCLUDED.uri
        AND current_record.source_event_id = EXCLUDED.source_event_id
        AND current_record.deleted_at IS NULL
  )
RETURNING repository_uri;

-- name: TombstoneFederationIssue :one
UPDATE network.issues AS issue SET
    cid = NULL,
    indexed_at = $2,
    deleted_at = $2,
    source_event_id = $3
WHERE issue.uri = $1
  AND issue.source_event_id < $3
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = $1
        AND current_record.source_event_id = $3
        AND current_record.deleted_at IS NOT NULL
  )
RETURNING repository_uri;

-- name: UpsertFederationIssueStatus :one
INSERT INTO network.issue_statuses (
    uri, cid, author_did, rkey, issue_uri, issue_cid, repository_uri,
    repository_cid, state, record_created_at, record_updated_at, indexed_at,
    deleted_at, source_event_id
)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NULL, $13
FROM network.records AS source_record
WHERE source_record.uri = $1
  AND source_record.source_event_id = $13
  AND source_record.deleted_at IS NULL
  AND (
      $7::text IS NULL
      OR NOT EXISTS (
          SELECT 1 FROM network.issue_comments AS parent
          WHERE parent.uri = $7 AND parent.deleted_at IS NULL
      )
      OR EXISTS (
          SELECT 1 FROM network.issue_comments AS parent
          WHERE parent.uri = $7
            AND parent.deleted_at IS NULL
            AND parent.issue_uri = $5
      )
  )
ON CONFLICT (uri) DO UPDATE SET
    cid = EXCLUDED.cid,
    author_did = EXCLUDED.author_did,
    rkey = EXCLUDED.rkey,
    issue_uri = EXCLUDED.issue_uri,
    issue_cid = EXCLUDED.issue_cid,
    repository_uri = EXCLUDED.repository_uri,
    repository_cid = EXCLUDED.repository_cid,
    state = EXCLUDED.state,
    record_created_at = EXCLUDED.record_created_at,
    record_updated_at = EXCLUDED.record_updated_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.issue_statuses.source_event_id < EXCLUDED.source_event_id
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = EXCLUDED.uri
        AND current_record.source_event_id = EXCLUDED.source_event_id
        AND current_record.deleted_at IS NULL
  )
RETURNING issue_uri, repository_uri;

-- name: TombstoneFederationIssueStatus :one
UPDATE network.issue_statuses AS status SET
    cid = NULL,
    indexed_at = $2,
    deleted_at = $2,
    source_event_id = $3
WHERE status.uri = $1
  AND status.source_event_id < $3
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = $1
        AND current_record.source_event_id = $3
        AND current_record.deleted_at IS NOT NULL
  )
RETURNING issue_uri, repository_uri;

-- name: RecomputeFederationIssueState :exec
WITH resolved AS (
    SELECT
        issue.uri,
        status.uri AS status_uri,
        status.cid AS status_cid,
        status.state,
        status.record_updated_at AS status_updated_at,
        status.source_event_id AS status_source_event_id
    FROM network.issues AS issue
    LEFT JOIN LATERAL (
        SELECT candidate.uri, candidate.cid, candidate.state,
               candidate.record_updated_at, candidate.source_event_id
        FROM network.issue_statuses AS candidate
        WHERE candidate.issue_uri = issue.uri
          AND candidate.repository_uri = issue.repository_uri
          AND candidate.author_did = split_part(issue.repository_uri, '/', 3)
          AND candidate.deleted_at IS NULL
          AND candidate.cid IS NOT NULL
        ORDER BY candidate.source_event_id DESC, candidate.uri DESC
        LIMIT 1
    ) AS status ON TRUE
    WHERE issue.uri = $1
)
UPDATE network.issues AS issue SET
    state = COALESCE(resolved.state, 'open'),
    status_uri = resolved.status_uri,
    status_cid = resolved.status_cid,
    status_updated_at = resolved.status_updated_at,
    status_source_event_id = resolved.status_source_event_id
FROM resolved
WHERE issue.uri = resolved.uri;

-- name: RecomputeFederationIssueCounts :exec
UPDATE network.repositories AS repository SET
    issue_count = (
        SELECT count(*) FROM network.issues AS issue
        WHERE issue.repository_uri = $1 AND issue.deleted_at IS NULL
    ),
    open_issue_count = (
        SELECT count(*) FROM network.issues AS issue
        WHERE issue.repository_uri = $1 AND issue.deleted_at IS NULL AND issue.state = 'open'
    )
WHERE repository.uri = $1;

-- name: UpsertFederationIssueComment :one
INSERT INTO network.issue_comments (
    uri, cid, author_did, rkey, issue_uri, issue_cid, parent_uri, parent_cid,
    body, record_created_at, record_updated_at, indexed_at, deleted_at, source_event_id
)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NULL, $13
FROM network.records AS source_record
WHERE source_record.uri = $1
  AND source_record.source_event_id = $13
  AND source_record.deleted_at IS NULL
ON CONFLICT (uri) DO UPDATE SET
    cid = EXCLUDED.cid,
    issue_cid = EXCLUDED.issue_cid,
    parent_cid = EXCLUDED.parent_cid,
    body = EXCLUDED.body,
    record_created_at = EXCLUDED.record_created_at,
    record_updated_at = EXCLUDED.record_updated_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.issue_comments.source_event_id < EXCLUDED.source_event_id
  AND network.issue_comments.author_did = EXCLUDED.author_did
  AND network.issue_comments.rkey = EXCLUDED.rkey
  AND network.issue_comments.issue_uri = EXCLUDED.issue_uri
  AND network.issue_comments.parent_uri IS NOT DISTINCT FROM EXCLUDED.parent_uri
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = EXCLUDED.uri
        AND current_record.source_event_id = EXCLUDED.source_event_id
        AND current_record.deleted_at IS NULL
  )
RETURNING issue_uri;

-- name: TombstoneFederationIssueComment :one
UPDATE network.issue_comments AS comment SET
    cid = NULL,
    indexed_at = $2,
    deleted_at = $2,
    source_event_id = $3
WHERE comment.uri = $1
  AND comment.source_event_id < $3
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = $1
        AND current_record.source_event_id = $3
        AND current_record.deleted_at IS NOT NULL
  )
RETURNING issue_uri;

-- name: RecomputeFederationIssueCommentCount :exec
UPDATE network.issues SET comment_count = (
    SELECT count(*) FROM network.issue_comments AS comment
    WHERE comment.issue_uri = $1
      AND comment.deleted_at IS NULL
      AND (
          comment.parent_uri IS NULL
          OR NOT EXISTS (
              SELECT 1 FROM network.issue_comments AS parent
              WHERE parent.uri = comment.parent_uri AND parent.deleted_at IS NULL
          )
          OR EXISTS (
              SELECT 1 FROM network.issue_comments AS parent
              WHERE parent.uri = comment.parent_uri
                AND parent.deleted_at IS NULL
                AND parent.issue_uri = comment.issue_uri
          )
      )
)
WHERE uri = $1;

-- name: RecomputeFederationRepositoryCommentCount :exec
UPDATE network.repositories AS repository SET comment_count = (
    SELECT count(*)
    FROM network.issue_comments AS comment
    JOIN network.issues AS issue ON issue.uri = comment.issue_uri
    WHERE issue.repository_uri = $1
      AND issue.deleted_at IS NULL
      AND comment.deleted_at IS NULL
      AND (
          comment.parent_uri IS NULL
          OR NOT EXISTS (
              SELECT 1 FROM network.issue_comments AS parent
              WHERE parent.uri = comment.parent_uri AND parent.deleted_at IS NULL
          )
          OR EXISTS (
              SELECT 1 FROM network.issue_comments AS parent
              WHERE parent.uri = comment.parent_uri
                AND parent.deleted_at IS NULL
                AND parent.issue_uri = comment.issue_uri
          )
      )
)
WHERE repository.uri = $1;

-- name: ListNetworkRepositories :many
SELECT
    repository.uri,
	repository.cid,
	repository.star_count,
	repository.issue_count,
	repository.open_issue_count,
	repository.comment_count,
    repository.local_repository_id,
    repository.owner_did,
    identity.handle,
    repository.slug,
    repository.name,
    repository.description,
    repository.default_branch,
    repository.git_https,
    repository.git_ssh,
    repository.web,
    repository.record_created_at,
    repository.record_updated_at,
    repository.indexed_at
FROM network.repositories AS repository
LEFT JOIN network.identities AS identity ON identity.did = repository.owner_did
WHERE repository.deleted_at IS NULL
  AND (
      sqlc.narg(cursor_indexed_at)::timestamptz IS NULL
      OR repository.indexed_at < sqlc.narg(cursor_indexed_at)
      OR (
          repository.indexed_at = sqlc.narg(cursor_indexed_at)
          AND repository.uri < sqlc.narg(cursor_uri)::text
      )
  )
ORDER BY repository.indexed_at DESC, repository.uri DESC
LIMIT sqlc.arg(page_size);

-- name: GetNetworkRepositoryStarTarget :one
SELECT uri, cid, star_count
FROM network.repositories
WHERE uri = $1 AND deleted_at IS NULL AND cid IS NOT NULL;

-- name: GetNetworkStarProjection :many
SELECT
    repository.uri AS repository_uri,
    repository.star_count,
    COALESCE(projected_star.uri, '') AS star_uri,
    COALESCE(projected_star.cid, '') AS star_cid,
    COALESCE(projected_star.author_did, '') AS author_did,
    COALESCE(projected_star.repository_cid, '') AS observed_repository_cid,
    COALESCE(projected_star.record_created_at, repository.indexed_at) AS record_created_at,
    COALESCE(projected_star.indexed_at, repository.indexed_at) AS indexed_at
FROM network.repositories AS repository
LEFT JOIN LATERAL (
    SELECT star.uri, star.cid, star.author_did, star.repository_cid, star.record_created_at, star.indexed_at
    FROM network.stars AS star
    WHERE star.repository_uri = repository.uri
      AND star.deleted_at IS NULL
      AND star.cid IS NOT NULL
    ORDER BY star.record_created_at DESC, star.uri DESC
    LIMIT sqlc.arg(page_size)
) AS projected_star ON TRUE
WHERE repository.uri = sqlc.arg(repository_uri)
  AND repository.deleted_at IS NULL
  AND repository.cid IS NOT NULL
ORDER BY projected_star.record_created_at DESC, projected_star.uri DESC;

-- name: GetNetworkIssueRepositoryTarget :one
SELECT uri, cid
FROM network.repositories
WHERE uri = $1 AND deleted_at IS NULL AND cid IS NOT NULL;

-- name: GetNetworkIssueStatusWriteTarget :one
SELECT
    issue.uri AS issue_uri,
    issue.cid AS issue_cid,
    repository.uri AS repository_uri,
    repository.cid AS repository_cid,
    status.record_created_at AS status_created_at
FROM network.issues AS issue
JOIN network.repositories AS repository
  ON repository.uri = issue.repository_uri
 AND repository.deleted_at IS NULL
 AND repository.cid IS NOT NULL
LEFT JOIN network.issue_statuses AS status
  ON status.uri = issue.status_uri
 AND status.cid = issue.status_cid
 AND status.deleted_at IS NULL
WHERE issue.uri = $1
  AND issue.deleted_at IS NULL
  AND issue.cid IS NOT NULL;

-- name: GetNetworkIssueProjection :many
SELECT
    repository.issue_count,
    repository.open_issue_count,
    repository.comment_count,
    COALESCE(projected_issue.uri, '') AS issue_uri,
    COALESCE(projected_issue.cid, '') AS issue_cid,
    COALESCE(projected_issue.author_did, '') AS author_did,
    repository.uri AS repository_uri,
    COALESCE(projected_issue.repository_cid, '') AS observed_repository_cid,
    COALESCE(projected_issue.title, '') AS title,
    COALESCE(projected_issue.body, '') AS body,
    COALESCE(projected_issue.state, 'open') AS state,
    COALESCE(projected_issue.status_uri, '') AS status_uri,
    COALESCE(projected_issue.status_cid, '') AS status_cid,
    COALESCE(projected_issue.record_created_at, repository.indexed_at) AS record_created_at,
    COALESCE(projected_issue.record_updated_at, repository.indexed_at) AS record_updated_at,
    COALESCE(projected_issue.indexed_at, repository.indexed_at) AS indexed_at
FROM network.repositories AS repository
LEFT JOIN LATERAL (
    SELECT issue.uri, issue.cid, issue.author_did, issue.repository_cid, issue.title, issue.body,
           issue.state, issue.status_uri, issue.status_cid, issue.record_created_at,
           issue.record_updated_at, issue.indexed_at
    FROM network.issues AS issue
    WHERE issue.repository_uri = repository.uri
      AND issue.deleted_at IS NULL
      AND issue.cid IS NOT NULL
    ORDER BY issue.record_created_at DESC, issue.uri DESC
    LIMIT sqlc.arg(page_size)
) AS projected_issue ON TRUE
WHERE repository.uri = sqlc.arg(repository_uri)
  AND repository.deleted_at IS NULL
  AND repository.cid IS NOT NULL
ORDER BY projected_issue.record_created_at DESC, projected_issue.uri DESC;

-- name: ListNetworkIssueComments :many
WITH target AS (
    SELECT issue.uri
    FROM network.issues AS issue
    WHERE issue.uri = sqlc.arg(issue_uri)
      AND issue.deleted_at IS NULL
      AND issue.cid IS NOT NULL
), visible AS MATERIALIZED (
    SELECT comment.*
    FROM network.issue_comments AS comment
    JOIN target ON target.uri = comment.issue_uri
    WHERE comment.deleted_at IS NULL
  AND comment.cid IS NOT NULL
  AND (
      comment.parent_uri IS NULL
      OR NOT EXISTS (
          SELECT 1 FROM network.issue_comments AS parent
          WHERE parent.uri = comment.parent_uri AND parent.deleted_at IS NULL
      )
      OR EXISTS (
          SELECT 1 FROM network.issue_comments AS parent
          WHERE parent.uri = comment.parent_uri
            AND parent.deleted_at IS NULL
            AND parent.issue_uri = comment.issue_uri
      )
  )
      AND (
          sqlc.narg(account_did)::text IS NULL
          OR (
              NOT EXISTS (
                  SELECT 1 FROM moderation.blocked_dids AS blocked
                  WHERE blocked.account_did = sqlc.narg(account_did)
                    AND blocked.blocked_did = comment.author_did
              )
              AND NOT EXISTS (
                  SELECT 1 FROM moderation.hidden_records AS hidden
                  WHERE hidden.account_did = sqlc.narg(account_did)
                    AND hidden.record_uri = comment.uri
              )
          )
      )
)
SELECT
    (SELECT count(*) FROM visible) AS comment_count,
    COALESCE(projected.uri, '') AS comment_uri,
    COALESCE(projected.cid, '') AS comment_cid,
    COALESCE(projected.author_did, '') AS author_did,
    COALESCE(projected.issue_uri, target.uri) AS issue_uri,
    COALESCE(projected.issue_cid, '') AS issue_cid,
    COALESCE(projected.parent_uri, '') AS parent_uri,
    COALESCE(projected.parent_cid, '') AS parent_cid,
    COALESCE(projected.body, '') AS body,
    projected.record_created_at,
    projected.record_updated_at,
    projected.indexed_at
FROM target
LEFT JOIN LATERAL (
    SELECT visible.*
    FROM visible
    ORDER BY visible.record_created_at, visible.uri
    LIMIT sqlc.arg(page_size)
) AS projected ON TRUE
ORDER BY projected.record_created_at, projected.uri;

-- name: GetNetworkIssueCommentTarget :one
SELECT issue.uri, issue.cid
FROM network.issues AS issue
WHERE issue.uri = $1
  AND issue.deleted_at IS NULL
  AND issue.cid IS NOT NULL;

-- name: GetNetworkIssueCommentParentTarget :one
SELECT comment.uri, comment.cid, comment.issue_uri
FROM network.issue_comments AS comment
JOIN network.issues AS issue
  ON issue.uri = comment.issue_uri
 AND issue.deleted_at IS NULL
 AND issue.cid IS NOT NULL
WHERE comment.uri = $1
  AND comment.deleted_at IS NULL
  AND comment.cid IS NOT NULL;

-- name: GetFederationPullRequestTargetRepositoryURI :one
SELECT target_repository_uri FROM network.pull_requests WHERE uri = $1;

-- name: GetProjectedPullRequestFetchTarget :one
SELECT
    pull_request.uri,
    pull_request.cid,
    source_repository.git_https,
    pull_request.source_branch,
    pull_request.head_sha,
    pull_request.target_branch,
    target_repository.local_repository_id
FROM network.pull_requests AS pull_request
JOIN network.repositories AS source_repository
  ON source_repository.uri = pull_request.source_repository_uri
 AND source_repository.cid = pull_request.source_repository_cid
 AND source_repository.deleted_at IS NULL
 AND source_repository.cid IS NOT NULL
 AND source_repository.git_https IS NOT NULL
 AND source_repository.git_https LIKE 'https://%'
JOIN network.repositories AS target_repository
  ON target_repository.uri = pull_request.target_repository_uri
 AND target_repository.cid = pull_request.target_repository_cid
 AND target_repository.deleted_at IS NULL
 AND target_repository.cid IS NOT NULL
 AND target_repository.local_repository_id IS NOT NULL
WHERE pull_request.uri = sqlc.arg(pull_request_uri)
  AND pull_request.deleted_at IS NULL
  AND pull_request.cid IS NOT NULL;

-- name: GetProjectedPullRequestMergeTarget :one
SELECT
    pull_request.uri,
    pull_request.cid,
    pull_request.source_repository_uri,
    pull_request.source_repository_cid,
    pull_request.source_branch,
    pull_request.head_sha,
    pull_request.target_repository_uri,
    pull_request.target_repository_cid,
    pull_request.target_branch,
    pull_request.title,
    pull_request.body,
    pull_request.record_created_at,
    target_repository.owner_did AS target_owner_did,
    target_repository.local_repository_id,
    COALESCE(status.state, 'open') AS state,
    status.record_created_at AS status_created_at
FROM network.pull_requests AS pull_request
JOIN network.repositories AS target_repository
  ON target_repository.uri = pull_request.target_repository_uri
 AND target_repository.cid = pull_request.target_repository_cid
 AND target_repository.deleted_at IS NULL
 AND target_repository.cid IS NOT NULL
 AND target_repository.local_repository_id IS NOT NULL
JOIN core.repositories AS local_repository
  ON local_repository.id = target_repository.local_repository_id
 AND local_repository.at_uri = target_repository.uri
 AND local_repository.at_cid = target_repository.cid
 AND local_repository.owner_did = target_repository.owner_did
 AND local_repository.state = 'active'
 AND local_repository.deleted_at IS NULL
LEFT JOIN network.pull_request_statuses AS status
  ON status.uri = pull_request.status_uri
 AND status.cid = pull_request.status_cid
 AND status.pull_request_uri = pull_request.uri
 AND status.pull_request_cid = pull_request.cid
 AND status.deleted_at IS NULL
 AND status.cid IS NOT NULL
WHERE pull_request.uri = sqlc.arg(pull_request_uri)
  AND pull_request.deleted_at IS NULL
  AND pull_request.cid IS NOT NULL;

-- name: GetProjectedPullRequestRepositoryTargets :one
SELECT source.uri AS source_uri, source.cid AS source_cid,
       target.uri AS target_uri, target.cid AS target_cid
FROM network.repositories AS source
CROSS JOIN network.repositories AS target
WHERE source.uri = sqlc.arg(source_repository_uri)
  AND source.deleted_at IS NULL AND source.cid IS NOT NULL
  AND target.uri = sqlc.arg(target_repository_uri)
  AND target.deleted_at IS NULL AND target.cid IS NOT NULL;

-- name: GetProjectedPullRequestCounts :one
SELECT pull_request_count, open_pull_request_count
FROM network.repositories
WHERE uri = sqlc.arg(repository_uri) AND deleted_at IS NULL AND cid IS NOT NULL;

-- name: ListProjectedPullRequests :many
SELECT pull_request.uri, pull_request.cid, pull_request.author_did,
       pull_request.source_repository_uri, pull_request.source_repository_cid, pull_request.source_branch,
       pull_request.target_repository_uri, pull_request.target_repository_cid, pull_request.target_branch,
       pull_request.head_sha, pull_request.title, pull_request.body,
       COALESCE(status.state, 'open') AS state, status.uri AS status_uri, status.cid AS status_cid,
       status.merged_commit_sha,
       (SELECT count(*) FROM network.pull_request_reviews AS review
        WHERE review.pull_request_uri = pull_request.uri AND review.pull_request_cid = pull_request.cid
          AND review.deleted_at IS NULL AND review.cid IS NOT NULL) AS review_count,
       pull_request.record_created_at, pull_request.record_updated_at, pull_request.indexed_at
FROM network.pull_requests AS pull_request
LEFT JOIN network.pull_request_statuses AS status
  ON status.uri = pull_request.status_uri AND status.cid = pull_request.status_cid
 AND status.pull_request_uri = pull_request.uri AND status.pull_request_cid = pull_request.cid
 AND status.deleted_at IS NULL AND status.cid IS NOT NULL
WHERE pull_request.target_repository_uri = sqlc.arg(repository_uri)
  AND pull_request.deleted_at IS NULL AND pull_request.cid IS NOT NULL
ORDER BY pull_request.record_created_at DESC, pull_request.uri DESC
LIMIT sqlc.arg(result_limit);

-- name: GetProjectedPullRequest :one
SELECT pull_request.uri, pull_request.cid, pull_request.author_did,
       pull_request.source_repository_uri, pull_request.source_repository_cid, pull_request.source_branch,
       pull_request.target_repository_uri, pull_request.target_repository_cid, pull_request.target_branch,
       pull_request.head_sha, pull_request.title, pull_request.body,
       COALESCE(status.state, 'open') AS state, status.uri AS status_uri, status.cid AS status_cid,
       status.merged_commit_sha,
       (SELECT count(*) FROM network.pull_request_reviews AS review
        WHERE review.pull_request_uri = pull_request.uri AND review.pull_request_cid = pull_request.cid
          AND review.deleted_at IS NULL AND review.cid IS NOT NULL) AS review_count,
       pull_request.record_created_at, pull_request.record_updated_at, pull_request.indexed_at
FROM network.pull_requests AS pull_request
LEFT JOIN network.pull_request_statuses AS status
  ON status.uri = pull_request.status_uri AND status.cid = pull_request.status_cid
 AND status.pull_request_uri = pull_request.uri AND status.pull_request_cid = pull_request.cid
 AND status.deleted_at IS NULL AND status.cid IS NOT NULL
WHERE pull_request.uri = sqlc.arg(pull_request_uri)
  AND pull_request.deleted_at IS NULL AND pull_request.cid IS NOT NULL;

-- name: ListProjectedPullRequestReviews :many
SELECT review.uri, review.cid, review.author_did, review.pull_request_uri, review.pull_request_cid,
       review.verdict, review.body, review.record_created_at, review.record_updated_at, review.indexed_at
FROM network.pull_request_reviews AS review
JOIN network.pull_requests AS pull_request
  ON pull_request.uri = review.pull_request_uri
 AND pull_request.cid = review.pull_request_cid
 AND pull_request.deleted_at IS NULL AND pull_request.cid IS NOT NULL
WHERE pull_request.uri = sqlc.arg(pull_request_uri)
  AND review.deleted_at IS NULL AND review.cid IS NOT NULL
ORDER BY review.record_created_at, review.uri
LIMIT sqlc.arg(result_limit);

-- name: GetProjectedPullRequestReviewTarget :one
SELECT uri, cid FROM network.pull_requests
WHERE uri = sqlc.arg(pull_request_uri) AND deleted_at IS NULL AND cid IS NOT NULL;

-- name: GetProjectedPullRequestStatusTarget :one
SELECT pull_request.uri, pull_request.cid, pull_request.target_repository_uri,
       pull_request.target_repository_cid, status.record_created_at AS status_created_at
FROM network.pull_requests AS pull_request
LEFT JOIN network.pull_request_statuses AS status
  ON status.uri = pull_request.status_uri
 AND status.cid = pull_request.status_cid
 AND status.pull_request_uri = pull_request.uri
 AND status.pull_request_cid = pull_request.cid
 AND status.deleted_at IS NULL AND status.cid IS NOT NULL
WHERE pull_request.uri = sqlc.arg(pull_request_uri)
  AND pull_request.deleted_at IS NULL AND pull_request.cid IS NOT NULL;

-- name: GetFederationPullRequestStatusTarget :one
SELECT pull_request_uri, target_repository_uri FROM network.pull_request_statuses WHERE uri = $1;

-- name: GetFederationPullRequestReviewSubject :one
SELECT pull_request_uri FROM network.pull_request_reviews WHERE uri = $1;

-- name: ListFederationRepositoryPullRequestURIs :many
SELECT uri FROM network.pull_requests WHERE target_repository_uri = $1 ORDER BY uri;

-- name: LockFederationPullRequest :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(pull_request_uri), 1886547817));

-- name: LockFederationRepositoryPullRequests :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(repository_uri), 1886547823));

-- name: UpsertFederationPullRequest :one
INSERT INTO network.pull_requests (
    uri, cid, author_did, rkey, source_repository_uri, source_repository_cid,
    source_branch, target_repository_uri, target_repository_cid, target_branch,
    head_sha, title, body, record_created_at, record_updated_at, indexed_at,
    deleted_at, source_event_id
)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
       $14, $15, $16, NULL, $17
FROM network.records AS source_record
WHERE source_record.uri = $1 AND source_record.source_event_id = $17 AND source_record.deleted_at IS NULL
ON CONFLICT (uri) DO UPDATE SET
    cid = EXCLUDED.cid,
    source_repository_cid = EXCLUDED.source_repository_cid,
    target_repository_cid = EXCLUDED.target_repository_cid,
    head_sha = EXCLUDED.head_sha,
    title = EXCLUDED.title,
    body = EXCLUDED.body,
    record_created_at = EXCLUDED.record_created_at,
    record_updated_at = EXCLUDED.record_updated_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.pull_requests.source_event_id < EXCLUDED.source_event_id
  AND network.pull_requests.author_did = EXCLUDED.author_did
  AND network.pull_requests.rkey = EXCLUDED.rkey
  AND network.pull_requests.source_repository_uri = EXCLUDED.source_repository_uri
  AND network.pull_requests.source_branch = EXCLUDED.source_branch
  AND network.pull_requests.target_repository_uri = EXCLUDED.target_repository_uri
  AND network.pull_requests.target_branch = EXCLUDED.target_branch
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = EXCLUDED.uri
        AND current_record.source_event_id = EXCLUDED.source_event_id
        AND current_record.deleted_at IS NULL
  )
RETURNING target_repository_uri;

-- name: TombstoneFederationPullRequest :one
UPDATE network.pull_requests AS pull_request SET
    cid = NULL, indexed_at = $2, deleted_at = $2, source_event_id = $3
WHERE pull_request.uri = $1
  AND pull_request.source_event_id < $3
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = $1 AND current_record.source_event_id = $3 AND current_record.deleted_at IS NOT NULL
  )
RETURNING target_repository_uri;

-- name: UpsertFederationPullRequestStatus :one
INSERT INTO network.pull_request_statuses (
    uri, cid, author_did, rkey, pull_request_uri, pull_request_cid,
    target_repository_uri, target_repository_cid, state, merged_commit_sha,
    record_created_at, record_updated_at, indexed_at, deleted_at, source_event_id
)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, sqlc.narg(merged_commit_sha),
       $10, $11, $12, NULL, $13
FROM network.records AS source_record
WHERE source_record.uri = $1 AND source_record.source_event_id = $13 AND source_record.deleted_at IS NULL
ON CONFLICT (uri) DO UPDATE SET
    cid = EXCLUDED.cid,
    pull_request_cid = EXCLUDED.pull_request_cid,
    target_repository_cid = EXCLUDED.target_repository_cid,
    state = EXCLUDED.state,
    merged_commit_sha = EXCLUDED.merged_commit_sha,
    record_created_at = EXCLUDED.record_created_at,
    record_updated_at = EXCLUDED.record_updated_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.pull_request_statuses.source_event_id < EXCLUDED.source_event_id
  AND network.pull_request_statuses.author_did = EXCLUDED.author_did
  AND network.pull_request_statuses.rkey = EXCLUDED.rkey
  AND network.pull_request_statuses.pull_request_uri = EXCLUDED.pull_request_uri
  AND network.pull_request_statuses.target_repository_uri = EXCLUDED.target_repository_uri
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = EXCLUDED.uri
        AND current_record.source_event_id = EXCLUDED.source_event_id
        AND current_record.deleted_at IS NULL
  )
RETURNING pull_request_uri, target_repository_uri;

-- name: TombstoneFederationPullRequestStatus :one
UPDATE network.pull_request_statuses AS status SET
    cid = NULL, indexed_at = $2, deleted_at = $2, source_event_id = $3
WHERE status.uri = $1
  AND status.source_event_id < $3
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = $1 AND current_record.source_event_id = $3 AND current_record.deleted_at IS NOT NULL
  )
RETURNING pull_request_uri, target_repository_uri;

-- name: UpsertFederationPullRequestReview :one
INSERT INTO network.pull_request_reviews (
    uri, cid, author_did, rkey, pull_request_uri, pull_request_cid, body, verdict,
    record_created_at, record_updated_at, indexed_at, deleted_at, source_event_id
)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULL, $12
FROM network.records AS source_record
WHERE source_record.uri = $1 AND source_record.source_event_id = $12 AND source_record.deleted_at IS NULL
ON CONFLICT (uri) DO UPDATE SET
    cid = EXCLUDED.cid,
    pull_request_cid = EXCLUDED.pull_request_cid,
    body = EXCLUDED.body,
    verdict = EXCLUDED.verdict,
    record_created_at = EXCLUDED.record_created_at,
    record_updated_at = EXCLUDED.record_updated_at,
    indexed_at = EXCLUDED.indexed_at,
    deleted_at = NULL,
    source_event_id = EXCLUDED.source_event_id
WHERE network.pull_request_reviews.source_event_id < EXCLUDED.source_event_id
  AND network.pull_request_reviews.author_did = EXCLUDED.author_did
  AND network.pull_request_reviews.rkey = EXCLUDED.rkey
  AND network.pull_request_reviews.pull_request_uri = EXCLUDED.pull_request_uri
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = EXCLUDED.uri
        AND current_record.source_event_id = EXCLUDED.source_event_id
        AND current_record.deleted_at IS NULL
  )
RETURNING pull_request_uri;

-- name: TombstoneFederationPullRequestReview :one
UPDATE network.pull_request_reviews AS review SET
    cid = NULL, indexed_at = $2, deleted_at = $2, source_event_id = $3
WHERE review.uri = $1
  AND review.source_event_id < $3
  AND EXISTS (
      SELECT 1 FROM network.records AS current_record
      WHERE current_record.uri = $1 AND current_record.source_event_id = $3 AND current_record.deleted_at IS NOT NULL
  )
RETURNING pull_request_uri;

-- name: RecomputeFederationPullRequestState :exec
WITH resolved AS (
    SELECT pull_request.uri, status.uri AS status_uri, status.cid AS status_cid,
           status.state, status.merged_commit_sha, status.record_updated_at AS status_updated_at,
           status.source_event_id AS status_source_event_id
    FROM network.pull_requests AS pull_request
    LEFT JOIN LATERAL (
        SELECT candidate.uri, candidate.cid, candidate.state, candidate.merged_commit_sha,
               candidate.record_updated_at, candidate.source_event_id
        FROM network.pull_request_statuses AS candidate
        WHERE candidate.pull_request_uri = pull_request.uri
          AND candidate.pull_request_cid = pull_request.cid
          AND candidate.target_repository_uri = pull_request.target_repository_uri
          AND candidate.author_did = split_part(pull_request.target_repository_uri, '/', 3)
          AND candidate.deleted_at IS NULL
          AND candidate.cid IS NOT NULL
        ORDER BY candidate.source_event_id DESC, candidate.uri DESC
        LIMIT 1
    ) AS status ON TRUE
    WHERE pull_request.uri = $1
)
UPDATE network.pull_requests AS pull_request SET
    state = COALESCE(resolved.state, 'open'),
    status_uri = resolved.status_uri,
    status_cid = resolved.status_cid,
    status_updated_at = resolved.status_updated_at,
    status_source_event_id = resolved.status_source_event_id,
    merged_commit_sha = resolved.merged_commit_sha
FROM resolved
WHERE pull_request.uri = resolved.uri;

-- name: RecomputeFederationPullRequestReviewCount :exec
UPDATE network.pull_requests SET review_count = (
    SELECT count(*) FROM network.pull_request_reviews AS review
    WHERE review.pull_request_uri = $1
      AND review.pull_request_cid = network.pull_requests.cid
      AND review.deleted_at IS NULL
)
WHERE uri = $1;

-- name: RecomputeFederationPullRequestCounts :exec
UPDATE network.repositories AS repository SET
    pull_request_count = (
        SELECT count(*) FROM network.pull_requests AS pull_request
        WHERE pull_request.target_repository_uri = $1 AND pull_request.deleted_at IS NULL
    ),
    open_pull_request_count = (
        SELECT count(*) FROM network.pull_requests AS pull_request
        WHERE pull_request.target_repository_uri = $1
          AND pull_request.deleted_at IS NULL AND pull_request.state = 'open'
    )
WHERE repository.uri = $1;
