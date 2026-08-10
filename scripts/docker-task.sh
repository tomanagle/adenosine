#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

task="${1:-}"
if [[ -z "$task" ]]; then
  echo "usage: $0 <e2e|e2e-federation>" >&2
  exit 2
fi

compose=(docker compose --env-file .env.local -f dev/docker-compose.yml)
if [[ "$task" != "e2e-federation" ]]; then
  ./scripts/ensure-dev-env.sh
  "${compose[@]}" build adenosine
fi

case "$task" in
  e2e)
    "${compose[@]}" up --build --detach --wait postgres otel-lgtm adenosine electric web gateway
    public_url="$(grep '^ADENOSINE_BASE_URL=' .env.local | cut -d= -f2-)"
    curl -fsS "$public_url/health/ready" >/dev/null
    curl -fsS "$public_url/" | grep -q "Build in public"
    ;;
  e2e-federation)
    project="adenosine-federation-$PPID-$$"
    export POSTGRES_USER="${POSTGRES_USER:-unused}" POSTGRES_DB="${POSTGRES_DB:-unused}" POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-unused}"
    export DATABASE_URL="${DATABASE_URL:-unused}" ADENOSINE_BASE_URL="${ADENOSINE_BASE_URL:-unused}" ADENOSINE_LISTEN_ADDR="${ADENOSINE_LISTEN_ADDR:-unused}"
    export ADENOSINE_REPO_ROOT="${ADENOSINE_REPO_ROOT:-unused}" ADENOSINE_OAUTH_STATE_KEY="${ADENOSINE_OAUTH_STATE_KEY:-unused}"
    export ADENOSINE_OAUTH_CREDENTIAL_KEY="${ADENOSINE_OAUTH_CREDENTIAL_KEY:-unused}" TAP_ADMIN_PASSWORD="${TAP_ADMIN_PASSWORD:-unused}"
    export ELECTRIC_SECRET="${ELECTRIC_SECRET:-unused}" ELECTRIC_DATABASE_PASSWORD="${ELECTRIC_DATABASE_PASSWORD:-unused}"
    federation_compose=(docker compose --project-name "$project" --profile federation-test -f dev/docker-compose.yml)
    cleanup() {
      status=$?
      if [[ $status -ne 0 ]]; then
        "${federation_compose[@]}" logs --no-color postgres-a postgres-b adenosine-a adenosine-a-tls adenosine-b adenosine-b-tls >&2 || true
      fi
      "${federation_compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
      trap - EXIT INT TERM
      exit "$status"
    }
    trap cleanup EXIT INT TERM

    "${federation_compose[@]}" build adenosine-a adenosine-b federation-acceptance federation-star federation-issue federation-comment federation-pr
    "${federation_compose[@]}" up --detach --wait postgres-a postgres-b adenosine-a adenosine-b adenosine-a-tls adenosine-b-tls
    "${federation_compose[@]}" exec -T adenosine-a go run ./test/federationhost
    "${federation_compose[@]}" exec -T adenosine-b go run ./test/federationhost -instance=b
    "${federation_compose[@]}" run --rm federation-acceptance go run ./test/federation -phase=seed
    "${federation_compose[@]}" run --rm federation-star
    "${federation_compose[@]}" run --rm federation-acceptance go run ./test/federation -phase=star
    "${federation_compose[@]}" run --rm federation-issue
    "${federation_compose[@]}" run --rm federation-acceptance go run ./test/federation -phase=issue
    "${federation_compose[@]}" run --rm federation-comment go run ./test/federationcomment -phase=create
    "${federation_compose[@]}" run --rm federation-acceptance go run ./test/federation -phase=comments
    "${federation_compose[@]}" run --rm federation-comment go run ./test/federationcomment -phase=moderate
    "${federation_compose[@]}" run --rm federation-comment go run ./test/federationcomment -phase=delete
    "${federation_compose[@]}" run --rm federation-acceptance go run ./test/federation -phase=comments-deleted
    "${federation_compose[@]}" run --rm federation-pr go run ./test/federationpr -phase=create
    "${federation_compose[@]}" exec -T adenosine-a go run ./test/federationpr -phase=fetch
    "${federation_compose[@]}" run --rm federation-pr go run ./test/federationpr -phase=merge
    "${federation_compose[@]}" stop adenosine-a postgres-a
    "${federation_compose[@]}" run --rm --no-deps federation-pr go run ./test/federationpr -phase=final
    "${federation_compose[@]}" run --rm --no-deps federation-acceptance go run ./test/federation -phase=final
    ;;
  *)
    echo "unknown task: $task" >&2
    exit 2
    ;;
esac
