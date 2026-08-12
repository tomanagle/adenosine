#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../deploy/lib.sh
source "$root/deploy/lib.sh"

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  echo "Usage: ADENOSINE_ENV_FILE=deploy/.env scripts/migrate.sh"
  exit 0
fi
[[ $# -eq 0 ]] || die "migrate takes no arguments"
need_tools docker
load_env
validate_environment
maintenance_owner=0
completed=0
cleanup() {
  if [[ $maintenance_owner -eq 1 && $completed -eq 1 ]]; then
    leave_maintenance >/dev/null || printf 'warning: maintenance remains active; inspect the stack before removing %s\n' "$MAINTENANCE_FILE" >&2
  elif [[ $maintenance_owner -eq 1 ]]; then
    printf 'Migration failed; maintenance remains active at %s.\n' "$MAINTENANCE_FILE" >&2
  fi
}
trap cleanup EXIT
if ! maintenance_active; then
  enter_maintenance
  maintenance_owner=1
fi
compose up -d --wait postgres otel-collector
compose run --rm --no-deps app migrate
if [[ -n "${ADENOSINE_ELECTRIC_URL:-}" ]]; then
  compose exec -T postgres psql -v ON_ERROR_STOP=1 -v electric_password="$ELECTRIC_DATABASE_PASSWORD" -U "$POSTGRES_USER" -d "$POSTGRES_DB" <<'SQL'
SELECT 'CREATE ROLE electric LOGIN REPLICATION'
WHERE NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'electric') \gexec
ALTER ROLE electric WITH LOGIN REPLICATION NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD :'electric_password';
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
WHERE NOT EXISTS (SELECT FROM pg_catalog.pg_publication WHERE pubname = 'electric_publication_default') \gexec
ALTER PUBLICATION electric_publication_default SET TABLE network.repositories, network.profiles,
  network.stars, network.issues, network.issue_comments, network.pull_requests,
  network.pull_request_reviews;
SQL
fi
completed=1
echo "Database migrations are current for $ADENOSINE_VERSION."
