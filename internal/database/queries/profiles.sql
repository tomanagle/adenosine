-- name: GetProfile :one
SELECT *
FROM network.profiles
WHERE did = $1
  AND deleted_at IS NULL;
