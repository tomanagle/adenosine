#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/adenosine-production-restore.XXXXXX")"
env_file="$temporary/production.env"
project="adenosine-restore-test-$PPID-$$"
image="adenosine-production-restore-test"
cleanup() {
  ADENOSINE_ENV_FILE="$env_file" docker compose --env-file "$env_file" -f "$root/deploy/docker-compose.yml" down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$root/deploy/.maintenance"
  rm -rf "$temporary"
}
trap cleanup EXIT

docker build --target production -f "$root/dev/Dockerfile" -t "$image:v0.1.0" "$root"
"$root/scripts/bootstrap.sh" --env-file "$env_file" --domain localhost --version v0.1.0 --env-only >/dev/null
printf 'ADENOSINE_IMAGE=%s\n' "$image" >> "$env_file.override"
while IFS= read -r line; do
  case "$line" in
    ADENOSINE_IMAGE=*) printf 'ADENOSINE_IMAGE=%s\n' "$image" ;;
    ADENOSINE_COMPOSE_PROJECT=*) printf 'ADENOSINE_COMPOSE_PROJECT=%s\n' "$project" ;;
    ADENOSINE_DOMAIN=*) printf 'ADENOSINE_DOMAIN=localhost\n' ;;
    ADENOSINE_BASE_URL=*) printf 'ADENOSINE_BASE_URL=https://localhost\n' ;;
    ADENOSINE_HTTP_PORT=*) printf 'ADENOSINE_HTTP_PORT=18080\n' ;;
    ADENOSINE_HTTPS_PORT=*) printf 'ADENOSINE_HTTPS_PORT=18443\n' ;;
    ADENOSINE_SSH_PORT=*) printf 'ADENOSINE_SSH_PORT=12222\n' ;;
    *) printf '%s\n' "$line" ;;
  esac
done < "$env_file" > "$env_file.override"
printf 'ADENOSINE_DOCTOR_PUBLIC_URL=https://localhost:18443\nADENOSINE_DOCTOR_CURL_INSECURE=1\n' >> "$env_file.override"
mv "$env_file.override" "$env_file"
export ADENOSINE_ENV_FILE="$env_file" ADENOSINE_SKIP_PULL=1

"$root/scripts/bootstrap.sh" --env-file "$env_file" >/dev/null
"$root/scripts/migrate.sh"
docker compose --env-file "$env_file" -f "$root/deploy/docker-compose.yml" up -d --wait postgres otel-collector app web
docker compose --env-file "$env_file" -f "$root/deploy/docker-compose.yml" exec -T web wget -qO- http://127.0.0.1:3000/ | grep -q 'Build in public'
docker compose --env-file "$env_file" -f "$root/deploy/docker-compose.yml" exec -T app wget -q --spider http://127.0.0.1:8080/health/ready

ADENOSINE_ENV_FILE="$env_file" ADENOSINE_MAINTENANCE_FILE="$root/deploy/.maintenance" bash -c 'source "$1/deploy/lib.sh"; load_env; enter_maintenance' _ "$root"
"$root/scripts/backup.sh" --output "$temporary/backups"
[[ -f "$root/deploy/.maintenance" ]]
[[ -z "$(docker compose --env-file "$env_file" -f "$root/deploy/docker-compose.yml" ps -q --status running caddy)" ]]
ADENOSINE_ENV_FILE="$env_file" ADENOSINE_MAINTENANCE_FILE="$root/deploy/.maintenance" bash -c 'source "$1/deploy/lib.sh"; load_env; leave_maintenance' _ "$root"
backup="$(printf '%s\n' "$temporary"/backups/*.tar.gz)"
"$root/scripts/restore.sh" --backup "$backup" --validate-only
docker compose --env-file "$env_file" -f "$root/deploy/docker-compose.yml" down -v --remove-orphans
rm -f "$env_file"

bad_work="$temporary/bad-backup"
mkdir "$bad_work"
tar -xzf "$backup" -C "$bad_work"
printf 'not a PostgreSQL dump\n' > "$bad_work/postgresql.dump"
(cd "$bad_work" && if command -v sha256sum >/dev/null 2>&1; then
  sha256sum manifest.json postgresql.dump repositories.tar.gz instance-state.tar.gz instance.env > SHA256SUMS
else
  shasum -a 256 manifest.json postgresql.dump repositories.tar.gz instance-state.tar.gz instance.env > SHA256SUMS
fi)
bad_backup="$temporary/bad-backup.tar.gz"
tar -czf "$bad_backup" -C "$bad_work" manifest.json SHA256SUMS postgresql.dump repositories.tar.gz instance-state.tar.gz instance.env
if "$root/scripts/restore.sh" --backup "$bad_backup" --env-file "$env_file" --confirm-clean >/dev/null 2>&1; then
  echo "restore accepted an invalid PostgreSQL dump" >&2
  exit 1
fi
[[ ! -e "$env_file" ]]
[[ -z "$(docker volume ls -q --filter "name=^${project}_postgres_data$")" ]]
[[ -z "$(docker volume ls -q --filter "name=^${project}_repositories$")" ]]
[[ -z "$(docker volume ls -q --filter "name=^${project}_instance_state$")" ]]

"$root/scripts/restore.sh" --backup "$backup" --env-file "$env_file" --confirm-clean
docker compose --env-file "$env_file" -f "$root/deploy/docker-compose.yml" exec -T web wget -qO- http://127.0.0.1:3000/ | grep -q 'Build in public'
docker compose --env-file "$env_file" -f "$root/deploy/docker-compose.yml" exec -T app wget -q --spider http://127.0.0.1:8080/health/ready

echo "ok: production Compose backup and clean restore"
