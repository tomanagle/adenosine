-- name: PageNotifications :many
WITH viewer AS (
  SELECT account.did, coalesce(account.handle_cache, identity.handle, '') AS handle
  FROM core.accounts AS account
  LEFT JOIN network.identities AS identity ON identity.did = account.did AND identity.is_active
  WHERE account.did = sqlc.arg(account_did)
), activity AS (
  SELECT md5('issue_comment:' || comment.uri)::uuid AS id,
         'issue_comment'::text AS kind, comment.author_did AS actor_did,
         issue.repository_uri, issue.uri AS subject_uri, 'issue'::text AS subject_kind,
         issue.title, comment.record_created_at AS occurred_at, comment.uri AS source_uri
  FROM network.issue_comments AS comment
  JOIN network.issues AS issue ON issue.uri = comment.issue_uri
  WHERE issue.author_did = sqlc.arg(account_did) AND comment.author_did <> sqlc.arg(account_did)
    AND comment.deleted_at IS NULL AND comment.cid IS NOT NULL
    AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL
  UNION ALL
  SELECT md5('pull_request_merged:' || status.uri)::uuid,
         'pull_request_merged'::text, status.author_did,
         pull.target_repository_uri, pull.uri, 'pull_request'::text,
         pull.title, status.record_created_at, status.uri
  FROM network.pull_request_statuses AS status
  JOIN network.pull_requests AS pull ON pull.uri = status.pull_request_uri
  WHERE pull.author_did = sqlc.arg(account_did) AND status.author_did <> sqlc.arg(account_did)
    AND status.state = 'merged' AND status.deleted_at IS NULL AND status.cid IS NOT NULL
    AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL
  UNION ALL
  SELECT md5('pull_request_review:' || review.uri)::uuid,
         'pull_request_review'::text, review.author_did,
         pull.target_repository_uri, pull.uri, 'pull_request'::text,
         pull.title, review.record_created_at, review.uri
  FROM network.pull_request_reviews AS review
  JOIN network.pull_requests AS pull ON pull.uri = review.pull_request_uri
  WHERE pull.author_did = sqlc.arg(account_did) AND review.author_did <> sqlc.arg(account_did)
    AND review.deleted_at IS NULL AND review.cid IS NOT NULL
    AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL
  UNION ALL
  SELECT md5('mention:' || issue.uri)::uuid,
         'mention'::text, issue.author_did, issue.repository_uri, issue.uri, 'issue'::text,
         issue.title, issue.record_created_at, issue.uri
  FROM network.issues AS issue CROSS JOIN viewer
  WHERE viewer.handle <> '' AND issue.author_did <> sqlc.arg(account_did)
    AND lower(issue.body) LIKE '%@' || lower(viewer.handle) || '%'
    AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL
  UNION ALL
  SELECT md5('mention:' || comment.uri)::uuid,
         'mention'::text, comment.author_did, issue.repository_uri, issue.uri, 'issue'::text,
         issue.title, comment.record_created_at, comment.uri
  FROM network.issue_comments AS comment
  JOIN network.issues AS issue ON issue.uri = comment.issue_uri CROSS JOIN viewer
  WHERE viewer.handle <> '' AND comment.author_did <> sqlc.arg(account_did)
    AND lower(comment.body) LIKE '%@' || lower(viewer.handle) || '%'
    AND comment.deleted_at IS NULL AND comment.cid IS NOT NULL
    AND issue.deleted_at IS NULL AND issue.cid IS NOT NULL
  UNION ALL
  SELECT md5('mention:' || pull.uri)::uuid,
         'mention'::text, pull.author_did, pull.target_repository_uri, pull.uri, 'pull_request'::text,
         pull.title, pull.record_created_at, pull.uri
  FROM network.pull_requests AS pull CROSS JOIN viewer
  WHERE viewer.handle <> '' AND pull.author_did <> sqlc.arg(account_did)
    AND lower(pull.body) LIKE '%@' || lower(viewer.handle) || '%'
    AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL
  UNION ALL
  SELECT md5('mention:' || review.uri)::uuid,
         'mention'::text, review.author_did, pull.target_repository_uri, pull.uri, 'pull_request'::text,
         pull.title, review.record_created_at, review.uri
  FROM network.pull_request_reviews AS review
  JOIN network.pull_requests AS pull ON pull.uri = review.pull_request_uri CROSS JOIN viewer
  WHERE viewer.handle <> '' AND review.author_did <> sqlc.arg(account_did)
    AND lower(review.body) LIKE '%@' || lower(viewer.handle) || '%'
    AND review.deleted_at IS NULL AND review.cid IS NOT NULL
    AND pull.deleted_at IS NULL AND pull.cid IS NOT NULL
)
SELECT activity.id, activity.kind, activity.actor_did, activity.repository_uri,
       activity.subject_uri, activity.subject_kind, activity.title, activity.occurred_at,
       coalesce(organization.slug, profile.handle, identity.handle, repository.owner_did)::text AS owner,
       repository.slug::text AS repository_slug,
       (state.read_at IS NOT NULL)::boolean AS read
FROM activity
JOIN network.repositories AS repository ON repository.uri = activity.repository_uri
LEFT JOIN network.profiles AS profile ON profile.did = repository.owner_did AND profile.deleted_at IS NULL
LEFT JOIN network.identities AS identity ON identity.did = repository.owner_did AND identity.is_active
LEFT JOIN network.organizations AS organization ON organization.uri = repository.organization_uri AND organization.deleted_at IS NULL
LEFT JOIN core.notification_states AS state
  ON state.account_did = sqlc.arg(account_did) AND state.notification_key = activity.id
WHERE repository.deleted_at IS NULL AND repository.cid IS NOT NULL
  AND state.dismissed_at IS NULL
  AND (NOT sqlc.arg(unread_only)::boolean OR state.read_at IS NULL)
  AND NOT EXISTS (
    SELECT 1 FROM moderation.blocked_dids AS block
    WHERE block.account_did = sqlc.arg(account_did) AND block.blocked_did = activity.actor_did
  )
  AND NOT EXISTS (
    SELECT 1 FROM moderation.hidden_records AS hidden
    WHERE hidden.account_did = sqlc.arg(account_did)
      AND hidden.record_uri IN (activity.source_uri, activity.subject_uri, activity.repository_uri)
  )
  AND (
    sqlc.narg(after_time)::timestamptz IS NULL
    OR (activity.occurred_at, activity.id) < (sqlc.narg(after_time)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY activity.occurred_at DESC, activity.id DESC
LIMIT sqlc.arg(page_limit);

-- name: PutNotificationReadState :exec
INSERT INTO core.notification_states (account_did, notification_key, read_at, updated_at)
VALUES (sqlc.arg(account_did), sqlc.arg(notification_key), sqlc.narg(read_at), sqlc.arg(updated_at))
ON CONFLICT (account_did, notification_key) DO UPDATE
SET read_at = excluded.read_at, updated_at = excluded.updated_at;

-- name: DismissNotification :exec
INSERT INTO core.notification_states (account_did, notification_key, dismissed_at, updated_at)
VALUES (sqlc.arg(account_did), sqlc.arg(notification_key), sqlc.arg(dismissed_at), sqlc.arg(dismissed_at))
ON CONFLICT (account_did, notification_key) DO UPDATE
SET dismissed_at = excluded.dismissed_at, updated_at = excluded.updated_at;
