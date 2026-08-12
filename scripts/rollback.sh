#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../deploy/lib.sh
source "$root/deploy/lib.sh"

usage() {
  echo "Usage: scripts/rollback.sh --version vX.Y.Z --confirm-schema-compatible"
}

target=""
confirmed=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) [[ $# -ge 2 ]] || die "--version requires a value"; target="$2"; shift 2 ;;
    --confirm-schema-compatible) confirmed=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done
[[ $confirmed -eq 1 ]] || die "--confirm-schema-compatible is required; rollback never reverses database migrations"
load_env
validate_environment
[[ -n "$target" ]] || die "--version is required"
[[ "$target" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] || die "rollback version is invalid"
current="$ADENOSINE_VERSION"
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
compose up -d app web caddy
"$root/scripts/doctor.sh"
printf 'Application rolled back from %s to %s. Database migrations were not reversed.\n' "$current" "$target"
