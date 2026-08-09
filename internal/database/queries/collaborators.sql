-- name: UpsertRepositoryCollaborator :one
INSERT INTO core.repository_collaborators (repository_id, account_did, role, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (repository_id, account_did) DO UPDATE SET
    role = EXCLUDED.role,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: GetRepositoryCollaborator :one
SELECT *
FROM core.repository_collaborators
WHERE repository_id = $1 AND account_did = $2;

-- name: CanWriteRepository :one
SELECT EXISTS (
    SELECT 1
    FROM core.repositories AS repository
    LEFT JOIN core.repository_collaborators AS collaborator
      ON collaborator.repository_id = repository.id
     AND collaborator.account_did = sqlc.arg(account_did)
    WHERE repository.id = sqlc.arg(repository_id)
      AND repository.deleted_at IS NULL
      AND (
        repository.owner_did = sqlc.arg(account_did)
        OR collaborator.role IN ('write', 'maintain', 'admin')
      )
);

-- name: CanReadRepository :one
SELECT EXISTS (
    SELECT 1
    FROM core.repositories AS repository
    LEFT JOIN core.repository_collaborators AS collaborator
      ON collaborator.repository_id = repository.id
     AND collaborator.account_did = sqlc.arg(account_did)
    WHERE repository.id = sqlc.arg(repository_id)
      AND repository.deleted_at IS NULL
      AND (
        repository.visibility = 'public'
        OR repository.owner_did = sqlc.arg(account_did)
        OR collaborator.role IN ('read', 'write', 'maintain', 'admin')
      )
);
