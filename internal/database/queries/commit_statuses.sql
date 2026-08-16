-- name: CreateCommitStatus :one
INSERT INTO core.commit_statuses (
    id, repository_id, commit_sha, context, state, description, target_url,
    creator_did, external_id, request_hash, created_at, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (repository_id, creator_did, external_id) DO NOTHING
RETURNING *;

-- name: GetCommitStatusByExternalID :one
SELECT *
FROM core.commit_statuses
WHERE repository_id = $1 AND creator_did = $2 AND external_id = $3;

-- name: PageCommitStatuses :many
SELECT *
FROM core.commit_statuses AS status
WHERE status.repository_id = sqlc.arg(repository_id)
  AND status.commit_sha = sqlc.arg(commit_sha)
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (status.created_at, status.id) < (
      SELECT cursor.created_at, cursor.id FROM core.commit_statuses AS cursor
      WHERE cursor.id = sqlc.narg(after_id)::uuid
        AND cursor.repository_id = sqlc.arg(repository_id)
        AND cursor.commit_sha = sqlc.arg(commit_sha)
    )
  )
ORDER BY status.created_at DESC, status.id DESC
LIMIT sqlc.arg(page_limit);

-- name: LatestCommitStatuses :many
SELECT DISTINCT ON (context) *
FROM core.commit_statuses
WHERE repository_id = $1 AND commit_sha = $2
ORDER BY context, id DESC;

-- name: CreateCheckRun :one
INSERT INTO core.check_runs (
    id, repository_id, commit_sha, name, external_id, creator_did, status,
    conclusion, details_url, output_title, output_summary, version,
    create_request_hash, started_at, completed_at, created_at, updated_at, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 1, $12, $13, $14, $15, $15, $16)
ON CONFLICT (repository_id, creator_did, external_id) DO NOTHING
RETURNING *;

-- name: GetCheckRun :one
SELECT *
FROM core.check_runs
WHERE id = $1 AND repository_id = $2;

-- name: GetCheckRunByExternalID :one
SELECT *
FROM core.check_runs
WHERE repository_id = $1 AND creator_did = $2 AND external_id = $3;

-- name: PageCheckRuns :many
SELECT *
FROM core.check_runs AS check_run
WHERE check_run.repository_id = sqlc.arg(repository_id)
  AND check_run.commit_sha = sqlc.arg(commit_sha)
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (check_run.created_at, check_run.id) < (
      SELECT cursor.created_at, cursor.id FROM core.check_runs AS cursor
      WHERE cursor.id = sqlc.narg(after_id)::uuid
        AND cursor.repository_id = sqlc.arg(repository_id)
        AND cursor.commit_sha = sqlc.arg(commit_sha)
    )
  )
ORDER BY check_run.created_at DESC, check_run.id DESC
LIMIT sqlc.arg(page_limit);

-- name: UpdateCheckRun :one
UPDATE core.check_runs
SET status = sqlc.arg(status),
    conclusion = sqlc.narg(conclusion),
    details_url = sqlc.narg(details_url),
    output_title = sqlc.arg(output_title),
    output_summary = sqlc.arg(output_summary),
    version = version + 1,
    started_at = sqlc.narg(started_at),
    completed_at = sqlc.narg(completed_at),
    updated_at = sqlc.arg(updated_at),
    expires_at = sqlc.arg(expires_at)
WHERE id = sqlc.arg(id)
  AND repository_id = sqlc.arg(repository_id)
  AND creator_did = sqlc.arg(creator_did)
  AND version = sqlc.arg(expected_version)
RETURNING *;

-- name: DeleteExpiredCommitStatuses :execrows
DELETE FROM core.commit_statuses AS expired
WHERE expired.expires_at <= sqlc.arg(expired_before)
  AND expired.id NOT IN (
      SELECT DISTINCT ON (repository_id, commit_sha, context) id
      FROM core.commit_statuses
      ORDER BY repository_id, commit_sha, context, id DESC
  );

-- name: DeleteExpiredCheckRuns :execrows
DELETE FROM core.check_runs AS expired
WHERE expired.expires_at <= sqlc.arg(expired_before)
  AND expired.status = 'completed'
  AND expired.id NOT IN (
      SELECT DISTINCT ON (repository_id, commit_sha, name) id
      FROM core.check_runs
      ORDER BY repository_id, commit_sha, name, id DESC
  );
