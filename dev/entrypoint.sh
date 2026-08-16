#!/usr/bin/env bash
set -euo pipefail

if [[ "${ADENOSINE_DEV_SERVICE:-}" == "web" ]]; then
  cd /workspace
  bun install --frozen-lockfile
  exec "$@"
fi

test -n "${DATABASE_URL:-}" || { echo "DATABASE_URL is required." >&2; exit 1; }
test -n "${ADENOSINE_REPO_ROOT:-}" || { echo "ADENOSINE_REPO_ROOT is required." >&2; exit 1; }
export ADENOSINE_RELEASE_ASSET_ROOT="${ADENOSINE_RELEASE_ASSET_ROOT:-/var/lib/adenosine/state/release-assets}"
test -n "${ADENOSINE_SSH_HOST_KEY_PATH:-}" || { echo "ADENOSINE_SSH_HOST_KEY_PATH is required." >&2; exit 1; }

mkdir -p "$ADENOSINE_REPO_ROOT"
mkdir -p "$ADENOSINE_RELEASE_ASSET_ROOT"
mkdir -p "$(dirname "$ADENOSINE_SSH_HOST_KEY_PATH")"
if [[ ! -f "$ADENOSINE_SSH_HOST_KEY_PATH" ]]; then
  ssh-keygen -q -t ed25519 -N "" -f "$ADENOSINE_SSH_HOST_KEY_PATH"
fi
chmod 600 "$ADENOSINE_SSH_HOST_KEY_PATH"
go mod download

if [[ "${1:-}" == "air" ]]; then
  go run ./cmd/adenosine migrate

  if [[ -n "${ELECTRIC_DATABASE_PASSWORD:-}" ]]; then
    psql "$DATABASE_URL" --quiet --set=ON_ERROR_STOP=1 \
      --set=electric_password="$ELECTRIC_DATABASE_PASSWORD" <<'SQL'
SELECT 'CREATE ROLE electric LOGIN REPLICATION'
WHERE NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'electric') \gexec

ALTER ROLE electric WITH LOGIN REPLICATION NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
  PASSWORD :'electric_password';
REVOKE ALL PRIVILEGES ON SCHEMA public, auth, core, network, moderation, ops FROM electric;
REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public, auth, core, network, moderation, ops FROM electric;
REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public, auth, core, network, moderation, ops FROM electric;
GRANT USAGE ON SCHEMA network TO electric;
GRANT SELECT ON TABLE network.repositories, network.profiles, network.stars, network.issues,
  network.issue_comments, network.pull_requests, network.pull_request_reviews TO electric;
ALTER TABLE network.repositories REPLICA IDENTITY FULL;
ALTER TABLE network.profiles REPLICA IDENTITY FULL;
ALTER TABLE network.stars REPLICA IDENTITY FULL;
ALTER TABLE network.issues REPLICA IDENTITY FULL;
ALTER TABLE network.issue_comments REPLICA IDENTITY FULL;
ALTER TABLE network.pull_requests REPLICA IDENTITY FULL;
ALTER TABLE network.pull_request_reviews REPLICA IDENTITY FULL;

SELECT 'CREATE PUBLICATION electric_publication_default'
WHERE NOT EXISTS (
  SELECT FROM pg_catalog.pg_publication WHERE pubname = 'electric_publication_default'
) \gexec
ALTER PUBLICATION electric_publication_default SET TABLE network.repositories, network.profiles,
  network.stars, network.issues, network.issue_comments, network.pull_requests,
  network.pull_request_reviews;
SQL
    unset ELECTRIC_DATABASE_PASSWORD
  fi
fi

exec "$@"
