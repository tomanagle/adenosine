#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

detach=""
if [[ "${1:-}" == "--detach" ]]; then
  detach="--detach"
elif [[ $# -gt 0 ]]; then
  echo "usage: $0 [--detach]" >&2
  exit 2
fi

command -v docker >/dev/null 2>&1 || { echo "Docker is required." >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "Docker is not running." >&2; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "Docker Compose is required." >&2; exit 1; }

./scripts/ensure-dev-env.sh

compose=(docker compose --env-file .env.local -f dev/docker-compose.yml)
if [[ -n "$detach" ]]; then
  "${compose[@]}" up --build --detach --wait --wait-timeout 120

  env_value() {
    local name="$1"
    local fallback="$2"
    local value
    value="$(grep -E "^${name}=" .env.local | cut -d= -f2- || true)"
    printf '%s' "${value:-$fallback}"
  }

  public_url="${ADENOSINE_BASE_URL:-$(env_value ADENOSINE_BASE_URL http://127.0.0.1:8080)}"
  echo "Adenosine UI:  $public_url"
  echo "API readiness: $public_url/health/ready"
  echo "Electric:      active (internal http://electric:3000)"
  echo "Adenosine SSH: ssh://git@localhost:${ADENOSINE_SSH_PORT:-$(env_value ADENOSINE_SSH_PORT 2222)}"
  echo "Grafana:       http://localhost:${GRAFANA_PORT:-$(env_value GRAFANA_PORT 3001)}"
  echo "Postgres:      localhost:${POSTGRES_PORT:-$(env_value POSTGRES_PORT 5432)}"
else
  "${compose[@]}" up --build
fi
