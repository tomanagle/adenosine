-- name: SearchRepositories :many
WITH candidates AS (
    SELECT repository.uri, repository.cid, repository.local_repository_id,
           repository.owner_did, coalesce(profile.handle, identity.handle) AS owner_handle,
           repository.slug, repository.name, repository.description, repository.default_branch,
           repository.git_https, repository.git_ssh, repository.web,
           repository.record_created_at, repository.record_updated_at, repository.indexed_at,
           (SELECT count(*) FROM network.stars star WHERE star.repository_uri = repository.uri AND star.deleted_at IS NULL AND star.cid IS NOT NULL AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = star.author_did) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = star.uri)) AS star_count,
           (SELECT count(*) FROM network.issues issue WHERE issue.repository_uri = repository.uri AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = issue.author_did) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = issue.uri)) AS issue_count,
           (SELECT count(*) FROM network.issues issue WHERE issue.repository_uri = repository.uri AND issue.state = 'open' AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = issue.author_did) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = issue.uri)) AS open_issue_count,
           (SELECT count(*) FROM network.issue_comments comment JOIN network.issues issue ON issue.uri = comment.issue_uri WHERE issue.repository_uri = repository.uri AND comment.deleted_at IS NULL AND comment.cid IS NOT NULL AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (issue.author_did, comment.author_did)) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (issue.uri, comment.uri))) AS comment_count,
           (SELECT count(*) FROM network.pull_requests pull WHERE pull.target_repository_uri = repository.uri AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = pull.author_did) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = pull.uri)) AS pull_request_count,
           (SELECT count(*) FROM network.pull_requests pull WHERE pull.target_repository_uri = repository.uri AND pull.state = 'open' AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = pull.author_did) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = pull.uri)) AS open_pull_request_count,
           GREATEST(
               ts_rank_cd(
                   to_tsvector('simple', coalesce(repository.name, '') || ' ' || coalesce(repository.slug, '') || ' ' || coalesce(repository.description, '')),
                   websearch_to_tsquery('simple', sqlc.arg(search_query)::text)
               )::double precision,
               similarity(lower(coalesce(repository.name, '')), lower(sqlc.arg(search_query)::text))::double precision,
               similarity(lower(coalesce(repository.slug, '')), lower(sqlc.arg(search_query)::text))::double precision,
               similarity(lower(coalesce(profile.handle, identity.handle, '')), lower(sqlc.arg(search_query)::text))::double precision
           )::double precision AS score
    FROM network.repositories AS repository
    LEFT JOIN network.profiles AS profile ON profile.did = repository.owner_did AND profile.deleted_at IS NULL
    LEFT JOIN network.identities AS identity ON identity.did = repository.owner_did AND identity.is_active
    LEFT JOIN core.repositories AS local_repository ON local_repository.id = repository.local_repository_id
    WHERE repository.deleted_at IS NULL
      AND repository.cid IS NOT NULL
      AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL))
      AND NOT EXISTS (
          SELECT 1 FROM moderation.blocked_dids AS block
          WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = repository.owner_did
      )
      AND NOT EXISTS (
          SELECT 1 FROM moderation.hidden_records AS hidden
          WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = repository.uri
      )
      AND (
          to_tsvector('simple', coalesce(repository.name, '') || ' ' || coalesce(repository.slug, '') || ' ' || coalesce(repository.description, ''))
              @@ websearch_to_tsquery('simple', sqlc.arg(search_query)::text)
           OR lower(coalesce(repository.name, '')) LIKE '%' || lower(sqlc.arg(search_pattern)::text) || '%' ESCAPE '\'
           OR lower(coalesce(repository.slug, '')) LIKE '%' || lower(sqlc.arg(search_pattern)::text) || '%' ESCAPE '\'
           OR lower(coalesce(repository.description, '')) LIKE '%' || lower(sqlc.arg(search_pattern)::text) || '%' ESCAPE '\'
           OR lower(coalesce(profile.handle, identity.handle, '')) LIKE '%' || lower(sqlc.arg(search_pattern)::text) || '%' ESCAPE '\'
      )
)
SELECT * FROM candidates
WHERE sqlc.narg(cursor_uri)::text IS NULL
   OR CASE WHEN sqlc.arg(search_sort)::text = 'relevance'
       THEN (score, indexed_at, uri) < (sqlc.narg(cursor_score)::double precision, sqlc.narg(cursor_indexed_at)::timestamptz, sqlc.narg(cursor_uri)::text)
       ELSE (indexed_at, uri) < (sqlc.narg(cursor_indexed_at)::timestamptz, sqlc.narg(cursor_uri)::text)
   END
ORDER BY
    CASE WHEN sqlc.arg(search_sort)::text = 'relevance' THEN score END DESC,
    indexed_at DESC,
    uri DESC
LIMIT sqlc.arg(page_size);

-- name: ResolveSearchRepository :one
SELECT repository.uri, repository.cid, repository.local_repository_id, repository.owner_did,
       repository.slug, repository.name, repository.description, repository.default_branch,
       repository.git_https, repository.git_ssh, repository.web, repository.record_created_at,
       repository.record_updated_at, repository.indexed_at,
       coalesce(profile.handle, identity.handle) AS owner_handle,
       (SELECT count(*) FROM network.stars AS star WHERE star.repository_uri = repository.uri AND star.deleted_at IS NULL AND star.cid IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = star.author_did)
          AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = star.uri)) AS star_count,
       (SELECT count(*) FROM network.issues AS issue WHERE issue.repository_uri = repository.uri AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = issue.author_did)
          AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = issue.uri)) AS issue_count,
       (SELECT count(*) FROM network.issues AS issue WHERE issue.repository_uri = repository.uri AND issue.state = 'open' AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = issue.author_did)
          AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = issue.uri)) AS open_issue_count,
       (SELECT count(*) FROM network.issue_comments comment JOIN network.issues issue ON issue.uri = comment.issue_uri WHERE issue.repository_uri = repository.uri AND comment.deleted_at IS NULL AND comment.cid IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (issue.author_did, comment.author_did))
          AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (issue.uri, comment.uri))) AS comment_count,
       (SELECT count(*) FROM network.pull_requests pull WHERE pull.target_repository_uri = repository.uri AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = pull.author_did)
          AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = pull.uri)) AS pull_request_count,
       (SELECT count(*) FROM network.pull_requests pull WHERE pull.target_repository_uri = repository.uri AND pull.state = 'open' AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = pull.author_did)
          AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = pull.uri)) AS open_pull_request_count
FROM network.repositories AS repository
LEFT JOIN network.profiles AS profile ON profile.did = repository.owner_did AND profile.deleted_at IS NULL
LEFT JOIN network.identities AS identity ON identity.did = repository.owner_did AND identity.is_active
LEFT JOIN core.repositories AS local_repository ON local_repository.id = repository.local_repository_id
WHERE repository.deleted_at IS NULL
  AND repository.cid IS NOT NULL
  AND lower(repository.slug) = lower(sqlc.arg(repository_slug)::text)
  AND (repository.owner_did = sqlc.arg(repository_owner)::text OR lower(coalesce(profile.handle, identity.handle, '')) = lower(sqlc.arg(repository_owner)::text))
  AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL))
  AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids AS block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = repository.owner_did)
  AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records AS hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = repository.uri)
ORDER BY repository.indexed_at DESC, repository.uri DESC
LIMIT 1;

-- name: ResolveSearchIssue :one
SELECT issue.*,
       (SELECT count(*) FROM network.issue_comments comment WHERE comment.issue_uri = issue.uri AND comment.deleted_at IS NULL AND comment.cid IS NOT NULL AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = comment.author_did) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = comment.uri)) AS visible_comment_count
FROM network.issues AS issue
JOIN network.repositories AS repository ON repository.uri = issue.repository_uri
LEFT JOIN core.repositories AS local_repository ON local_repository.id = repository.local_repository_id
WHERE issue.uri = sqlc.arg(issue_uri)::text
  AND issue.repository_uri = sqlc.arg(repository_uri)::text
  AND issue.deleted_at IS NULL
  AND issue.cid IS NOT NULL
  AND repository.deleted_at IS NULL
  AND repository.cid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids AS block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, issue.author_did))
  AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records AS hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, issue.uri))
  AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL))
  AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids AS block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = issue.author_did)
  AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records AS hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = issue.uri);

-- name: ResolveSearchProfile :one
SELECT profile.*,
       (SELECT count(*) FROM network.repositories repository LEFT JOIN core.repositories local_repository ON local_repository.id = repository.local_repository_id WHERE repository.owner_did = profile.did AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL)) AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = repository.owner_did) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = repository.uri)) AS visible_repository_count,
       ((SELECT count(*) FROM network.issues issue JOIN network.repositories repository ON repository.uri = issue.repository_uri LEFT JOIN core.repositories local_repository ON local_repository.id = repository.local_repository_id WHERE issue.author_did = profile.did AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL)) AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, issue.author_did)) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, issue.uri))) +
        (SELECT count(*) FROM network.issue_comments comment JOIN network.issues issue ON issue.uri = comment.issue_uri JOIN network.repositories repository ON repository.uri = issue.repository_uri LEFT JOIN core.repositories local_repository ON local_repository.id = repository.local_repository_id WHERE comment.author_did = profile.did AND comment.deleted_at IS NULL AND comment.cid IS NOT NULL AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL)) AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, issue.author_did, comment.author_did)) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, issue.uri, comment.uri))) +
        (SELECT count(*) FROM network.pull_requests pull JOIN network.repositories repository ON repository.uri = pull.target_repository_uri LEFT JOIN core.repositories local_repository ON local_repository.id = repository.local_repository_id WHERE pull.author_did = profile.did AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL)) AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, pull.author_did)) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, pull.uri))) +
        (SELECT count(*) FROM network.pull_request_reviews review JOIN network.pull_requests pull ON pull.uri = review.pull_request_uri AND pull.cid = review.pull_request_cid JOIN network.repositories repository ON repository.uri = pull.target_repository_uri LEFT JOIN core.repositories local_repository ON local_repository.id = repository.local_repository_id WHERE review.author_did = profile.did AND review.deleted_at IS NULL AND review.cid IS NOT NULL AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL)) AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, pull.author_did, review.author_did)) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, pull.uri, review.uri)))) AS visible_contribution_count
FROM network.profiles profile
LEFT JOIN network.identities identity ON identity.did = profile.did
WHERE profile.did = sqlc.arg(profile_did)::text AND profile.deleted_at IS NULL AND (identity.did IS NULL OR identity.is_active)
  AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = profile.did)
  AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = profile.profile_uri);

-- name: ListSearchIssues :many
SELECT issue.*,
       (SELECT count(*) FROM network.issue_comments comment WHERE comment.issue_uri = issue.uri AND comment.deleted_at IS NULL AND comment.cid IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = comment.author_did)
          AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = comment.uri)) AS visible_comment_count
FROM network.issues issue JOIN network.repositories repository ON repository.uri = issue.repository_uri
WHERE issue.repository_uri = sqlc.arg(repository_uri)::text AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, issue.author_did))
  AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, issue.uri))
ORDER BY issue.record_created_at DESC, issue.uri DESC LIMIT sqlc.arg(result_limit);

-- name: CountSearchIssues :one
SELECT count(*) AS visible_issue_count, count(*) FILTER (WHERE issue.state = 'open') AS visible_open_issue_count
FROM network.issues issue JOIN network.repositories repository ON repository.uri = issue.repository_uri
WHERE issue.repository_uri = sqlc.arg(repository_uri)::text AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, issue.author_did))
  AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, issue.uri));

-- name: ListSearchStars :many
SELECT star.*
FROM network.stars star JOIN network.repositories repository ON repository.uri = star.repository_uri
WHERE star.repository_uri = sqlc.arg(repository_uri)::text AND star.deleted_at IS NULL AND star.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, star.author_did))
  AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, star.uri))
ORDER BY star.record_created_at DESC, star.uri DESC LIMIT sqlc.arg(result_limit);

-- name: CountSearchStars :one
SELECT count(*) FROM network.stars star JOIN network.repositories repository ON repository.uri = star.repository_uri
WHERE star.repository_uri = sqlc.arg(repository_uri)::text AND star.deleted_at IS NULL AND star.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, star.author_did))
  AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, star.uri));

-- name: ListSearchPullRequests :many
SELECT pull.*,
       (SELECT count(*) FROM network.pull_request_reviews review WHERE review.pull_request_uri = pull.uri AND review.pull_request_cid = pull.cid AND review.deleted_at IS NULL AND review.cid IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = review.author_did)
          AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = review.uri)) AS visible_review_count
FROM network.pull_requests pull JOIN network.repositories repository ON repository.uri = pull.target_repository_uri
WHERE pull.target_repository_uri = sqlc.arg(repository_uri)::text AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, pull.author_did))
  AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, pull.uri))
ORDER BY pull.record_created_at DESC, pull.uri DESC LIMIT sqlc.arg(result_limit);

-- name: CountSearchPullRequests :one
SELECT count(*) AS visible_pull_request_count, count(*) FILTER (WHERE pull.state = 'open') AS visible_open_pull_request_count
FROM network.pull_requests pull JOIN network.repositories repository ON repository.uri = pull.target_repository_uri
WHERE pull.target_repository_uri = sqlc.arg(repository_uri)::text AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, pull.author_did))
  AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, pull.uri));

-- name: ResolveSearchPullRequest :one
SELECT pull.*,
       (SELECT count(*) FROM network.pull_request_reviews review WHERE review.pull_request_uri = pull.uri AND review.pull_request_cid = pull.cid AND review.deleted_at IS NULL AND review.cid IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = review.author_did)
          AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = review.uri)) AS visible_review_count
FROM network.pull_requests pull JOIN network.repositories repository ON repository.uri = pull.target_repository_uri
WHERE pull.uri = sqlc.arg(pull_request_uri)::text AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, pull.author_did))
  AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, pull.uri));

-- name: ListSearchPullRequestReviews :many
SELECT review.* FROM network.pull_request_reviews review
JOIN network.pull_requests pull ON pull.uri = review.pull_request_uri AND pull.cid = review.pull_request_cid
JOIN network.repositories repository ON repository.uri = pull.target_repository_uri
WHERE pull.uri = sqlc.arg(pull_request_uri)::text AND review.deleted_at IS NULL AND review.cid IS NOT NULL AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, pull.author_did, review.author_did))
  AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, pull.uri, review.uri))
ORDER BY review.record_created_at, review.uri LIMIT sqlc.arg(result_limit);

-- name: SearchProfiles :many
WITH candidates AS (
    SELECT profile.*,
           (SELECT count(*) FROM network.repositories repository LEFT JOIN core.repositories local_repository ON local_repository.id = repository.local_repository_id WHERE repository.owner_did = profile.did AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL)) AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = repository.owner_did) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = repository.uri)) AS visible_repository_count,
           ((SELECT count(*) FROM network.issues issue JOIN network.repositories repository ON repository.uri = issue.repository_uri LEFT JOIN core.repositories local_repository ON local_repository.id = repository.local_repository_id WHERE issue.author_did = profile.did AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL)) AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, issue.author_did)) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, issue.uri))) +
            (SELECT count(*) FROM network.issue_comments comment JOIN network.issues issue ON issue.uri = comment.issue_uri JOIN network.repositories repository ON repository.uri = issue.repository_uri LEFT JOIN core.repositories local_repository ON local_repository.id = repository.local_repository_id WHERE comment.author_did = profile.did AND comment.deleted_at IS NULL AND comment.cid IS NOT NULL AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL)) AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, issue.author_did, comment.author_did)) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, issue.uri, comment.uri))) +
            (SELECT count(*) FROM network.pull_requests pull JOIN network.repositories repository ON repository.uri = pull.target_repository_uri LEFT JOIN core.repositories local_repository ON local_repository.id = repository.local_repository_id WHERE pull.author_did = profile.did AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL)) AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, pull.author_did)) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, pull.uri))) +
            (SELECT count(*) FROM network.pull_request_reviews review JOIN network.pull_requests pull ON pull.uri = review.pull_request_uri AND pull.cid = review.pull_request_cid JOIN network.repositories repository ON repository.uri = pull.target_repository_uri LEFT JOIN core.repositories local_repository ON local_repository.id = repository.local_repository_id WHERE review.author_did = profile.did AND review.deleted_at IS NULL AND review.cid IS NOT NULL AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL AND repository.deleted_at IS NULL AND repository.cid IS NOT NULL AND (local_repository.id IS NULL OR (local_repository.visibility = 'public' AND local_repository.state = 'active' AND local_repository.deleted_at IS NULL)) AND NOT EXISTS (SELECT 1 FROM moderation.blocked_dids block WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did IN (repository.owner_did, pull.author_did, review.author_did)) AND NOT EXISTS (SELECT 1 FROM moderation.hidden_records hidden WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri IN (repository.uri, pull.uri, review.uri)))) AS visible_contribution_count,
           GREATEST(
               ts_rank_cd(
                   to_tsvector('simple', coalesce(profile.handle, '') || ' ' || coalesce(profile.display_name, '')),
                   websearch_to_tsquery('simple', sqlc.arg(search_query)::text)
               )::double precision,
               similarity(lower(coalesce(profile.handle, '')), lower(sqlc.arg(search_query)::text))::double precision,
               similarity(lower(coalesce(profile.display_name, '')), lower(sqlc.arg(search_query)::text))::double precision
           )::double precision AS score
    FROM network.profiles AS profile
    LEFT JOIN network.identities AS identity ON identity.did = profile.did
    WHERE profile.deleted_at IS NULL
      AND (identity.did IS NULL OR identity.is_active)
      AND NOT EXISTS (
          SELECT 1 FROM moderation.blocked_dids AS block
          WHERE block.account_did = sqlc.narg(viewer_did) AND block.blocked_did = profile.did
      )
      AND NOT EXISTS (
          SELECT 1 FROM moderation.hidden_records AS hidden
          WHERE hidden.account_did = sqlc.narg(viewer_did) AND hidden.record_uri = profile.profile_uri
      )
      AND (
          to_tsvector('simple', coalesce(profile.handle, '') || ' ' || coalesce(profile.display_name, ''))
              @@ websearch_to_tsquery('simple', sqlc.arg(search_query)::text)
           OR lower(coalesce(profile.handle, '')) LIKE '%' || lower(sqlc.arg(search_pattern)::text) || '%' ESCAPE '\'
           OR lower(coalesce(profile.display_name, '')) LIKE '%' || lower(sqlc.arg(search_pattern)::text) || '%' ESCAPE '\'
      )
)
SELECT * FROM candidates
WHERE sqlc.narg(cursor_did)::text IS NULL
   OR CASE WHEN sqlc.arg(search_sort)::text = 'relevance'
       THEN (score, indexed_at, did) < (sqlc.narg(cursor_score)::double precision, sqlc.narg(cursor_indexed_at)::timestamptz, sqlc.narg(cursor_did)::text)
       ELSE (indexed_at, did) < (sqlc.narg(cursor_indexed_at)::timestamptz, sqlc.narg(cursor_did)::text)
   END
ORDER BY
    CASE WHEN sqlc.arg(search_sort)::text = 'relevance' THEN score END DESC,
    indexed_at DESC,
    did DESC
LIMIT sqlc.arg(page_size);
