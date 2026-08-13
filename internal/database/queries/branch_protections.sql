-- name: CreateBranchProtection :one
INSERT INTO core.branch_protections (
  id, repository_id, pattern, deny_force_push, deny_deletion, required_approvals,
  dismiss_stale_reviews, required_status_checks, require_signed_commits, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
RETURNING *;

-- name: GetBranchProtection :one
SELECT * FROM core.branch_protections WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id);

-- name: PageBranchProtections :many
SELECT * FROM core.branch_protections AS protection
WHERE protection.repository_id = sqlc.arg(repository_id)
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (protection.created_at, protection.id) < (
      SELECT cursor.created_at, cursor.id FROM core.branch_protections AS cursor
      WHERE cursor.id = sqlc.narg(after_id)::uuid AND cursor.repository_id = sqlc.arg(repository_id)
    )
  )
ORDER BY protection.created_at DESC, protection.id DESC
LIMIT sqlc.arg(page_limit);

-- name: UpdateBranchProtection :one
UPDATE core.branch_protections
SET pattern = sqlc.arg(pattern), deny_force_push = sqlc.arg(deny_force_push),
    deny_deletion = sqlc.arg(deny_deletion), required_approvals = sqlc.arg(required_approvals),
    dismiss_stale_reviews = sqlc.arg(dismiss_stale_reviews),
    required_status_checks = sqlc.arg(required_status_checks),
    require_signed_commits = sqlc.arg(require_signed_commits), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id)
RETURNING *;

-- name: DeleteBranchProtection :execrows
DELETE FROM core.branch_protections WHERE id = sqlc.arg(id) AND repository_id = sqlc.arg(repository_id);

-- name: GetEffectiveReceiveProtection :one
SELECT coalesce(bool_or(deny_force_push), false)::boolean AS deny_force_push,
       coalesce(bool_or(deny_deletion), false)::boolean AS deny_deletion
FROM core.branch_protections
WHERE repository_id = $1;

-- name: ListBranchProtectionsForEvaluation :many
SELECT *
FROM core.branch_protections
WHERE repository_id = $1
ORDER BY pattern, id;

-- name: ListProtectedRepositoryIDs :many
SELECT DISTINCT repository_id
FROM core.branch_protections
ORDER BY repository_id;

-- name: GetBranchProtectionReviewSummary :one
WITH candidate AS (
  SELECT pull.uri, pull.cid, pull.author_did
  FROM network.pull_requests AS pull
  JOIN network.repositories AS projected_repository
    ON projected_repository.uri = pull.target_repository_uri
   AND projected_repository.deleted_at IS NULL
   AND projected_repository.cid IS NOT NULL
  WHERE projected_repository.local_repository_id = sqlc.arg(repository_id)
    AND pull.target_branch = sqlc.arg(target_branch)
    AND pull.head_sha = sqlc.arg(head_sha)
    AND pull.state = 'open'
    AND pull.deleted_at IS NULL
    AND pull.cid IS NOT NULL
  ORDER BY pull.record_updated_at DESC, pull.uri DESC
  LIMIT 1
), latest_reviews AS (
  SELECT DISTINCT ON (review.author_did) review.author_did, review.verdict
  FROM network.pull_request_reviews AS review
  JOIN candidate ON candidate.uri = review.pull_request_uri
  WHERE review.deleted_at IS NULL
    AND review.cid IS NOT NULL
    AND review.author_did <> candidate.author_did
    AND (NOT sqlc.arg(dismiss_stale_reviews)::boolean OR review.pull_request_cid = candidate.cid)
  ORDER BY review.author_did, review.record_updated_at DESC, review.uri DESC
)
SELECT candidate.uri AS pull_request_uri,
       count(*) FILTER (WHERE latest_reviews.verdict = 'approve')::integer AS approval_count,
       coalesce(bool_or(latest_reviews.verdict = 'request_changes'), false)::boolean AS changes_requested
FROM candidate
LEFT JOIN latest_reviews ON TRUE
GROUP BY candidate.uri;
