#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

command -v docker >/dev/null 2>&1 || { echo "Docker is required." >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "Docker is not running." >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "curl is required." >&2; exit 1; }
test -f .env.local || { echo ".env.local is missing; run make dev first." >&2; exit 1; }

compose=(docker compose --env-file .env.local -f dev/docker-compose.yml)
public_url="$(grep '^ADENOSINE_BASE_URL=' .env.local | cut -d= -f2-)"
failed=0

check() {
  local label="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    printf 'ok: %s\n' "$label"
  else
    printf 'not healthy: %s\n' "$label" >&2
    failed=1
  fi
}

check_optional() {
  local label="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    printf 'ok: %s\n' "$label"
  else
    printf 'unavailable (optional): %s\n' "$label" >&2
  fi
}

check "Postgres" "${compose[@]}" exec -T postgres pg_isready -U "$(grep '^POSTGRES_USER=' .env.local | cut -d= -f2-)" -d "$(grep '^POSTGRES_DB=' .env.local | cut -d= -f2-)"
check "gateway SSR landing ($public_url)" sh -c 'curl -fsS "$1/" | grep -q "Build in public"' _ "$public_url"
check "API readiness through gateway" curl -fsS "$public_url/health/ready"
check "web internal SSR" "${compose[@]}" exec -T web sh -c 'wget -qO- http://127.0.0.1:3001/ | grep -q "Build in public"'
check "Adenosine internal readiness" "${compose[@]}" exec -T adenosine wget -qO- http://localhost:8080/health/ready
check "Tap" "${compose[@]}" exec -T tap sh -c 'credentials=$(printf "admin:%s" "$TAP_ADMIN_PASSWORD" | base64 | tr -d "\n"); wget -qO- --header="Authorization: Basic $credentials" http://127.0.0.1:2480/health'
check_optional "Electric realtime; REST/UI remain available" "${compose[@]}" exec -T electric sh -c 'test "$(curl -so /dev/null -w "%{http_code}" http://localhost:3000/v1/health)" = 200'

if [[ $failed -ne 0 ]]; then
  exit 1
fi
echo "Adenosine development environment is healthy."
