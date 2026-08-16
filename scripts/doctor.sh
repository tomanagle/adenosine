#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../deploy/lib.sh
source "$root/deploy/lib.sh"

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  echo "Usage: ADENOSINE_ENV_FILE=deploy/.env scripts/doctor.sh"
  exit 0
fi
[[ $# -eq 0 ]] || die "doctor takes no arguments"
need_tools docker curl
load_env
validate_environment
failed=0

check() {
  local label="$1"
  shift
  if "$@" >/dev/null 2>&1; then printf 'ok: %s\n' "$label"; else printf 'failed: %s\n' "$label" >&2; failed=1; fi
}
warn() {
  local label="$1"
  shift
  if "$@" >/dev/null 2>&1; then printf 'ok: %s\n' "$label"; else printf 'warning: %s\n' "$label" >&2; fi
}

check "PostgreSQL accepts connections" compose exec -T postgres pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"
check "schema migrations table exists" compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atqc "SELECT 1 FROM public.schema_migrations LIMIT 1"
if maintenance_active; then
  printf 'maintenance: public readiness checks are intentionally unavailable\n'
  app_command=(compose run --rm --no-deps --entrypoint "" app)
else
  public_url="${ADENOSINE_DOCTOR_PUBLIC_URL:-$ADENOSINE_BASE_URL}"
  curl_options=(-fsS --max-time 15)
  if [[ "${ADENOSINE_DOCTOR_CURL_INSECURE:-}" == "1" ]]; then curl_options+=(-k); fi
  check "application readiness through HTTPS" curl "${curl_options[@]}" "$public_url/health/ready"
  check "OpenAPI is served" curl "${curl_options[@]}" "$public_url/openapi.yaml"
  app_command=(compose exec -T app)
fi
check "native Git is available" "${app_command[@]}" git --version
check "repository volume is writable" "${app_command[@]}" sh -ec 'test -d "$ADENOSINE_REPO_ROOT" && test -w "$ADENOSINE_REPO_ROOT"'
if [[ "${ADENOSINE_RELEASE_ASSET_BACKEND:-filesystem}" == filesystem ]]; then
  check "release asset volume is writable" "${app_command[@]}" sh -ec 'test -d "$ADENOSINE_RELEASE_ASSET_ROOT" && test -w "$ADENOSINE_RELEASE_ASSET_ROOT"'
else
  check "S3 release asset backend passed startup validation" "${app_command[@]}" sh -ec 'test "$ADENOSINE_RELEASE_ASSET_BACKEND" = s3'
fi
check "SSH host key exists with private permissions" "${app_command[@]}" sh -ec 'test -s "$ADENOSINE_SSH_HOST_KEY_PATH" && test "$(stat -c %a "$ADENOSINE_SSH_HOST_KEY_PATH")" = 600'
check "public URL matches configuration" "${app_command[@]}" sh -ec 'test "$ADENOSINE_BASE_URL" = "https://$ADENOSINE_SSH_HOST"'
check "disk space remains above 10 percent" "${app_command[@]}" sh -ec 'test "$(df -P "$ADENOSINE_REPO_ROOT" | tail -1 | tr -s " " | cut -d " " -f 5 | tr -d "%")" -lt 90'
if [[ "${ADENOSINE_RELEASE_ASSET_BACKEND:-filesystem}" == filesystem ]]; then
  check "release asset disk space remains above 10 percent" "${app_command[@]}" sh -ec 'test "$(df -P "$ADENOSINE_RELEASE_ASSET_ROOT" | tail -1 | tr -s " " | cut -d " " -f 5 | tr -d "%")" -lt 90'
fi
warn "OpenTelemetry Collector is reachable" "${app_command[@]}" wget -q --spider http://otel-collector:13133/
warn "outbound ATProto federation is reachable" "${app_command[@]}" wget -q --spider https://bsky.network/

if [[ -n "${ADENOSINE_TAP_CONSUMER:-}" ]]; then
  check "Tap is running" compose ps --status running tap
else
  printf 'skipped: Tap is not enabled\n'
fi
if [[ -n "${ADENOSINE_ELECTRIC_URL:-}" ]]; then
  check "Electric is reachable" compose exec -T app wget -q --spider http://electric:3000/v1/health
  check "Electric replication role and publication exist" compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atqc "SELECT 1 FROM pg_roles r, pg_publication p WHERE r.rolname='electric' AND r.rolreplication AND p.pubname='electric_publication_default'"
else
  printf 'skipped: Electric is not enabled; Git, REST, and federation remain available\n'
fi

[[ $failed -eq 0 ]] || exit 1
echo "Adenosine production deployment is healthy."
