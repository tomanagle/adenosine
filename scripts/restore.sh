#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../deploy/lib.sh
source "$root/deploy/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/restore.sh --backup FILE [--checksum FILE] [--env-file PATH] [--validate-only]
       scripts/restore.sh --backup FILE [--checksum FILE] [--env-file PATH] --confirm-clean

Validate a backup before mutation. A clean restore installs the backup environment and
preserves encryption secrets and SSH identity. Failed clean restores remove newly created
project volumes and restore the prior selected environment so they can be retried safely.
EOF
}

backup=""
checksum=""
confirmed=0
validate_only=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --backup) [[ $# -ge 2 ]] || die "--backup requires a file"; backup="$2"; shift 2 ;;
    --checksum) [[ $# -ge 2 ]] || die "--checksum requires a file"; checksum="$2"; shift 2 ;;
    --env-file) [[ $# -ge 2 ]] || die "--env-file requires a path"; ENV_FILE="$2"; shift 2 ;;
    --confirm-clean) confirmed=1; shift ;;
    --validate-only) validate_only=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done
[[ -n "$backup" && -f "$backup" ]] || die "--backup must name a backup archive"
if [[ $validate_only -eq 0 && $confirmed -eq 0 ]]; then die "--confirm-clean or --validate-only is required"; fi
need_tools docker jq tar
backup="$(cd "$(dirname "$backup")" && pwd)/$(basename "$backup")"
if [[ -z "$checksum" && -f "$backup.sha256" ]]; then checksum="$backup.sha256"; fi
if [[ -n "$checksum" ]]; then
  [[ -f "$checksum" ]] || die "outer checksum file does not exist"
  expected_outer="$(cut -d' ' -f1 < "$checksum")"
  [[ "$expected_outer" =~ ^[0-9a-fA-F]{64}$ ]] || die "outer checksum is invalid"
  actual_outer="$(sha256 "$backup" | cut -d' ' -f1)"
  [[ "$actual_outer" == "$expected_outer" ]] || die "outer backup checksum validation failed"
fi

umask 077
work="$(mktemp -d "${TMPDIR:-/tmp}/adenosine-restore.XXXXXX")"
target_env="$ENV_FILE"
env_previous="$work/previous.env"
env_existed=0
env_installed=0
mutation_started=0
completed=0
cleanup() {
  status=$?
  if [[ $mutation_started -eq 1 && $completed -eq 0 ]]; then
    compose down -v --remove-orphans >/dev/null 2>&1 || true
    rm -f "$MAINTENANCE_FILE"
  fi
  if [[ $env_installed -eq 1 && $completed -eq 0 ]]; then
    if [[ $env_existed -eq 1 ]]; then install -m 600 "$env_previous" "$target_env"; else rm -f "$target_env"; fi
    printf 'Restore failed; %s was restored and any newly populated clean project volumes were removed. Retry is safe.\n' "$target_env" >&2
  fi
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT

while IFS= read -r member; do
  [[ "$member" != /* && "$member" != *../* && "$member" != ../* ]] || die "backup contains an unsafe path"
done < <(tar -tzf "$backup")
tar -xzf "$backup" -C "$work"
for required in manifest.json SHA256SUMS postgresql.dump repositories.tar.gz instance-state.tar.gz instance.env; do
  [[ -f "$work/$required" ]] || die "backup is missing $required"
done
jq -e '
  type == "object" and
  .format == "adenosine-backup-v1" and
  (.created_at | type == "string") and
  (.release | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+([.-][A-Za-z0-9.-]+)?$")) and
  (.schema | type == "string" and test("^[0-9]{6}_[A-Za-z0-9_]+\\.sql$")) and
  .consistency == "maintenance-window" and
  .release_asset_backend == "filesystem" and
  .contents == ["manifest.json", "postgresql.dump", "repositories.tar.gz", "instance-state.tar.gz", "instance.env"]
' "$work/manifest.json" >/dev/null || die "backup manifest is malformed or unsupported"
checksum_files="$(while read -r digest file extra; do
  [[ "$digest" =~ ^[0-9a-fA-F]{64}$ && -n "$file" && -z "${extra:-}" ]] || die "backup checksum inventory is malformed"
  printf '%s\n' "${file#\*}"
done < "$work/SHA256SUMS")"
expected_files="$(printf '%s\n' manifest.json postgresql.dump repositories.tar.gz instance-state.tar.gz instance.env)"
[[ "$checksum_files" == "$expected_files" ]] || die "backup checksum inventory is invalid"
(cd "$work" && if command -v sha256sum >/dev/null 2>&1; then sha256sum -c SHA256SUMS; else shasum -a 256 -c SHA256SUMS; fi) >/dev/null || die "backup payload checksum validation failed"
for nested in repositories.tar.gz instance-state.tar.gz; do
  while IFS= read -r member; do
    [[ "$member" != /* && "$member" != *../* && "$member" != ../* ]] || die "$nested contains an unsafe path"
  done < <(tar -tzf "$work/$nested")
done

ENV_FILE="$work/instance.env"
load_env
validate_environment
require_portable_release_asset_backend
manifest_release="$(jq -r .release "$work/manifest.json")"
manifest_schema="$(jq -r .schema "$work/manifest.json")"
[[ "$manifest_release" == "$ADENOSINE_VERSION" ]] || die "manifest release does not match instance.env"
pull_service_images app postgres >/dev/null
docker run --rm --entrypoint test "$ADENOSINE_IMAGE:$manifest_release" -f "/opt/adenosine/migrations/$manifest_schema" || die "backup schema is not supported by $manifest_release"
printf 'Backup validation passed for release %s and schema %s.\n' "$manifest_release" "$manifest_schema"
if [[ $validate_only -eq 1 ]]; then completed=1; exit 0; fi

if [[ -f "$target_env" ]]; then cp "$target_env" "$env_previous"; env_existed=1; fi
mkdir -p "$(dirname "$target_env")"
install -m 600 "$work/instance.env" "$target_env"
env_installed=1
ENV_FILE="$target_env"
export ADENOSINE_ENV_FILE="$target_env"
export ADENOSINE_MAINTENANCE=1
mkdir -p "$(dirname "$MAINTENANCE_FILE")"
(umask 077; printf '%s\n' "restore" > "$MAINTENANCE_FILE")
compose down >/dev/null
compose run --rm --no-deps --entrypoint sh postgres -ec 'test -z "$(ls -A /var/lib/postgresql/data)"' || die "PostgreSQL volume is not empty"
compose run --rm --no-deps --user root --entrypoint sh app -ec 'test -z "$(ls -A /var/lib/adenosine/repos)" && test -z "$(ls -A /var/lib/adenosine/state)"' || die "repository or instance-state volume is not empty"
mutation_started=1
compose run --rm --no-deps --user root --entrypoint tar -v "$work:/restore:ro" app -xzf /restore/repositories.tar.gz -C /var/lib/adenosine/repos
compose run --rm --no-deps --user root --entrypoint tar -v "$work:/restore:ro" app -xzf /restore/instance-state.tar.gz -C /var/lib/adenosine/state
compose run --rm --no-deps --user root --entrypoint sh app -ec 'chown -R adenosine:adenosine /var/lib/adenosine/repos /var/lib/adenosine/state && chmod 600 /var/lib/adenosine/state/ssh_host_ed25519_key'
compose up -d postgres otel-collector
ready=0
for ((attempt = 1; attempt <= 60; attempt++)); do
  if compose exec -T postgres pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB" >/dev/null 2>&1; then ready=1; break; fi
  sleep 2
done
[[ $ready -eq 1 ]] || die "PostgreSQL did not become ready within 120 seconds"
compose exec -T postgres pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --exit-on-error --no-owner --no-privileges < "$work/postgresql.dump"
ADENOSINE_ENV_FILE="$target_env" "$root/scripts/migrate.sh"
ADENOSINE_ENV_FILE="$target_env" "$root/scripts/doctor.sh"
leave_maintenance
ADENOSINE_ENV_FILE="$target_env" "$root/scripts/doctor.sh"
completed=1
echo "Clean-volume restore completed. Reindex optional Electric and federation projections if enabled."
