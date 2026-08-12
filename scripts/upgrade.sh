#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../deploy/lib.sh
source "$root/deploy/lib.sh"

usage() {
  echo "Usage: scripts/upgrade.sh --version vX.Y.Z [--backup-dir DIRECTORY]"
}

target=""
backup_dir="$ROOT/backups"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) [[ $# -ge 2 ]] || die "--version requires a value"; target="$2"; shift 2 ;;
    --backup-dir) [[ $# -ge 2 ]] || die "--backup-dir requires a directory"; backup_dir="$2"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done
[[ "$target" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] || die "--version must be an immutable vX.Y.Z release"
need_tools docker curl
load_env
validate_environment
[[ "$target" != "$ADENOSINE_VERSION" ]] || die "$target is already deployed"
curl -fsS --max-time 20 "https://api.github.com/repos/tomanagle/adenosine/releases/tags/$target" >/dev/null || die "release notes for $target are unavailable"

previous="$ADENOSINE_VERSION"
"$root/scripts/doctor.sh"
maintenance_owner=0
completed=0
cleanup() {
  if [[ $maintenance_owner -eq 1 && $completed -eq 1 ]]; then
    leave_maintenance >/dev/null || printf 'warning: maintenance remains active; inspect the stack before removing %s\n' "$MAINTENANCE_FILE" >&2
  elif [[ $maintenance_owner -eq 1 ]]; then
    printf 'Upgrade failed; maintenance remains active at %s.\n' "$MAINTENANCE_FILE" >&2
  fi
}
trap cleanup EXIT
enter_maintenance
maintenance_owner=1
export ADENOSINE_MAINTENANCE=1
ADENOSINE_ENV_FILE="$ENV_FILE" "$root/scripts/backup.sh" --output "$backup_dir"
docker pull "$ADENOSINE_IMAGE:$target"

temporary="$(mktemp "${ENV_FILE}.XXXXXX")"
trap 'rm -f "$temporary"' EXIT
while IFS= read -r line || [[ -n "$line" ]]; do
  if [[ "$line" == ADENOSINE_VERSION=* ]]; then printf 'ADENOSINE_VERSION=%s\n' "$target"; else printf '%s\n' "$line"; fi
done < "$ENV_FILE" > "$temporary"
chmod 600 "$temporary"
mv "$temporary" "$ENV_FILE"
trap - EXIT
export ADENOSINE_VERSION="$target"

if ! ADENOSINE_ENV_FILE="$ENV_FILE" "$root/scripts/migrate.sh" || ! ADENOSINE_ENV_FILE="$ENV_FILE" "$root/scripts/doctor.sh"; then
  printf 'Upgrade to %s failed. Data was backed up. After checking schema compatibility, run:\n  scripts/rollback.sh --version %s --confirm-schema-compatible\n' "$target" "$previous" >&2
  exit 1
fi
leave_maintenance
maintenance_owner=0
completed=1
ADENOSINE_ENV_FILE="$ENV_FILE" "$root/scripts/doctor.sh"
printf 'Upgrade from %s to %s completed. Review release notes and retain the backup per policy.\n' "$previous" "$target"
