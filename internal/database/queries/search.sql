-- name: SearchRepositories :many
WITH candidates AS (
    SELECT repository.uri, repository.cid, repository.local_repository_id,
           repository.owner_did, coalesce(profile.handle, identity.handle) AS owner_handle,
           repository.slug, repository.name, repository.description, repository.default_branch,
           repository.git_https, repository.git_ssh, repository.web,
           repository.record_created_at, repository.record_updated_at, repository.indexed_at,
           repository.star_count, repository.issue_count, repository.open_issue_count,
           repository.comment_count, repository.pull_request_count, repository.open_pull_request_count,
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
          OR lower(coalesce(repository.name, '')) LIKE '%' || lower(sqlc.arg(search_query)::text) || '%'
          OR lower(coalesce(repository.slug, '')) LIKE '%' || lower(sqlc.arg(search_query)::text) || '%'
          OR lower(coalesce(repository.description, '')) LIKE '%' || lower(sqlc.arg(search_query)::text) || '%'
          OR lower(coalesce(profile.handle, identity.handle, '')) LIKE '%' || lower(sqlc.arg(search_query)::text) || '%'
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

-- name: SearchProfiles :many
WITH candidates AS (
    SELECT profile.*,
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
          OR lower(coalesce(profile.handle, '')) LIKE '%' || lower(sqlc.arg(search_query)::text) || '%'
          OR lower(coalesce(profile.display_name, '')) LIKE '%' || lower(sqlc.arg(search_query)::text) || '%'
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
