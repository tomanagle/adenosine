#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../deploy/lib.sh
source "$root/deploy/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/backup.sh [--output DIRECTORY]

Take a maintenance-window backup. The public application is stopped while PostgreSQL,
repositories, and instance state are captured. The resulting tar.gz contains secrets;
store it encrypted and restrict access.
EOF
}

output="$ROOT/backups"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output) [[ $# -ge 2 ]] || die "--output requires a directory"; output="$2"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done
need_tools docker tar
load_env
validate_environment
require_portable_release_asset_backend
compose exec -T postgres pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB" >/dev/null || die "PostgreSQL is not ready"

umask 077
mkdir -p "$output"
work="$(mktemp -d "${TMPDIR:-/tmp}/adenosine-backup.XXXXXX")"
maintenance_owner=0
completed=0
cleanup() {
  rm -rf "$work"
  if [[ $maintenance_owner -eq 1 && $completed -eq 1 ]]; then
    leave_maintenance >/dev/null || printf 'warning: maintenance remains active; inspect the stack before removing %s\n' "$MAINTENANCE_FILE" >&2
  elif [[ $maintenance_owner -eq 1 ]]; then
    printf 'Backup failed; maintenance remains active at %s.\n' "$MAINTENANCE_FILE" >&2
  fi
}
trap cleanup EXIT

if ! maintenance_active; then
  echo "Entering maintenance window. Pushes and state-changing requests will be unavailable."
  enter_maintenance
  maintenance_owner=1
else
  echo "Using the active maintenance window."
fi
schema_version="$(compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atqc "SELECT COALESCE(MAX(name), 'none') FROM public.schema_migrations")"
compose exec -T postgres pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom --no-owner --no-privileges > "$work/postgresql.dump"
cp "$ENV_FILE" "$work/instance.env"
chmod 600 "$work/instance.env"
compose run --rm --no-deps --user root --entrypoint tar -v "$work:/backup" app -czf /backup/repositories.tar.gz -C /var/lib/adenosine/repos . >/dev/null
compose run --rm --no-deps --user root --entrypoint tar -v "$work:/backup" app -czf /backup/instance-state.tar.gz -C /var/lib/adenosine/state . >/dev/null

created="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
cat > "$work/manifest.json" <<EOF
{
  "format": "adenosine-backup-v1",
  "created_at": "$created",
  "release": "$ADENOSINE_VERSION",
  "schema": "$schema_version",
  "consistency": "maintenance-window",
  "release_asset_backend": "filesystem",
  "rpo": "state committed before maintenance began",
  "contents": ["manifest.json", "postgresql.dump", "repositories.tar.gz", "instance-state.tar.gz", "instance.env"]
}
EOF
(cd "$work" && sha256 manifest.json postgresql.dump repositories.tar.gz instance-state.tar.gz instance.env > SHA256SUMS)
archive="$output/adenosine-${ADENOSINE_VERSION}-$(date -u +%Y%m%dT%H%M%SZ).tar.gz"
tar -czf "$archive" -C "$work" manifest.json SHA256SUMS postgresql.dump repositories.tar.gz instance-state.tar.gz instance.env
(cd "$output" && sha256 "$(basename "$archive")" > "$(basename "$archive").sha256")
chmod 600 "$archive" "$archive.sha256"
completed=1
echo "Backup written to $archive"
echo "Retention and off-host encryption are operator responsibilities; verify restore drills regularly."
