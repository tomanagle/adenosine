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
    project="adenosine-production-smoke-$PPID-$$"
    production_compose=(docker compose --project-name "$project" --env-file .env.local --profile production-smoke -f dev/docker-compose.yml)
    cleanup_production_smoke() {
      status=$?
      if [[ $status -ne 0 ]]; then
        echo "production image smoke test failed; container state and logs follow" >&2
        "${production_compose[@]}" ps --all >&2 || true
        "${production_compose[@]}" logs --no-color production-postgres production-adenosine >&2 || true
        production_container=$("${production_compose[@]}" ps --all --quiet production-adenosine 2>/dev/null || true)
        if [[ -n "$production_container" ]]; then
          docker inspect "$production_container" >&2 || true
        fi
      fi
      "${production_compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
      trap - EXIT INT TERM
      exit "$status"
    }
    trap cleanup_production_smoke EXIT INT TERM

    "${production_compose[@]}" build production-adenosine
    "${production_compose[@]}" up --detach --wait --wait-timeout 120 production-postgres production-adenosine

    production_image=$("${production_compose[@]}" images --quiet production-adenosine)
    test -n "$production_image"
    test "$(docker image inspect "$production_image" --format '{{.Config.User}}')" = adenosine
    test "$(docker image inspect "$production_image" --format '{{json .Config.Entrypoint}}')" = '["adenosine"]'
    test "$(docker image inspect "$production_image" --format '{{json .Config.Cmd}}')" = '["serve"]'
    test "$("${production_compose[@]}" exec -T production-adenosine id -un)" = adenosine
    test "$("${production_compose[@]}" exec -T production-adenosine id -u)" != 0
    "${production_compose[@]}" exec -T production-adenosine git --version
    "${production_compose[@]}" exec -T production-adenosine sh -ec '
      test -w "$ADENOSINE_REPO_ROOT"
      test -w "$(dirname "$ADENOSINE_SSH_HOST_KEY_PATH")"
      touch "$ADENOSINE_REPO_ROOT/.production-smoke-write"
      touch "$(dirname "$ADENOSINE_SSH_HOST_KEY_PATH")/.production-smoke-write"
      rm "$ADENOSINE_REPO_ROOT/.production-smoke-write" "$(dirname "$ADENOSINE_SSH_HOST_KEY_PATH")/.production-smoke-write"
      ! test -w /
      ! test -w /etc
      ! test -w /usr/local/bin
    '
    test "$("${production_compose[@]}" exec -T production-postgres psql -U adenosine -d adenosine -tAc 'SELECT count(*) > 0 FROM public.schema_migrations')" = t
    "${production_compose[@]}" exec -T production-adenosine sh -ec 'wget -S --spider http://localhost:8080/health/ready 2>&1 | grep -q "HTTP/1.1 200 OK"'
    "${production_compose[@]}" exec -T production-adenosine sh -ec 'wget -qO- http://localhost:8080/openapi.json | grep -q '"'"'"openapi"'"'"''

    "${production_compose[@]}" down --volumes --remove-orphans
    trap - EXIT INT TERM

    postgres_stopped=false
    cleanup_e2e() {
      status=$?
      if [[ $status -ne 0 ]]; then
        "${compose[@]}" ps --all >&2 || true
        "${compose[@]}" logs --no-color --tail 200 postgres adenosine gateway >&2 || true
      fi
      if [[ "$postgres_stopped" == true ]]; then
        "${compose[@]}" start postgres >/dev/null 2>&1 || true
      fi
      trap - EXIT INT TERM
      exit "$status"
    }
    trap cleanup_e2e EXIT INT TERM

    wait_for_status() {
      endpoint=$1
      expected=$2
      last_status=000
      for attempt in $(seq 1 60); do
        if ! last_status=$(curl --silent --show-error --max-time 2 --output /dev/null --write-out '%{http_code}' "$public_url$endpoint" 2>/dev/null); then
          last_status=000
        fi
        if [[ "$last_status" == "$expected" ]]; then
          return 0
        fi
        sleep 0.5
      done
      echo "timed out waiting for $endpoint to return $expected (last status: $last_status)" >&2
      return 1
    }

    application_identity() {
      container_id=$("${compose[@]}" ps --quiet adenosine)
      test -n "$container_id"
      container_identity=$(docker inspect --format '{{.Id}}:{{.State.Pid}}:{{.State.StartedAt}}:{{.RestartCount}}' "$container_id")
      application_pid=$("${compose[@]}" exec -T adenosine sh -c 'for process in /proc/[0-9]*; do if [ "$(readlink "$process/exe" 2>/dev/null || true)" = /workspace/.air/adenosine ]; then printf "%s\n" "${process##*/}"; fi; done')
      test -n "$application_pid"
      printf '%s:application-pid=%s\n' "$container_identity" "$application_pid"
    }
    # Recreate the reusable development containers so Vite and Air cannot retain
    # module or binary caches from a previous generated API contract.
    "${compose[@]}" up --build --detach --force-recreate --wait postgres otel-lgtm adenosine electric web gateway
    public_url="$(grep '^ADENOSINE_BASE_URL=' .env.local | cut -d= -f2-)"
    wait_for_status /health/live 200
    wait_for_status /health/ready 200
    curl -fsS "$public_url/" | grep -q "Build in public"
    identity_before=$(application_identity)

    postgres_stopped=true
    "${compose[@]}" stop postgres
    wait_for_status /health/live 200
    wait_for_status /health/ready 503

    "${compose[@]}" start postgres
    postgres_stopped=false
    wait_for_status /health/ready 200
    identity_after=$(application_identity)
    if [[ "$identity_after" != "$identity_before" ]]; then
      echo "Adenosine restarted during the PostgreSQL outage" >&2
      echo "before: $identity_before" >&2
      echo "after:  $identity_after" >&2
      false
    fi

    command -v jq >/dev/null 2>&1 || { echo "jq is required for release lifecycle e2e" >&2; exit 1; }
    release_repository_id="0198aaaa-0000-7000-8000-00000000ee10"
    release_token="adn_pat_release_e2e"
    release_token_hash=$(printf '%s' "$release_token" | openssl dgst -sha256 | awk '{print $NF}')
    "${compose[@]}" exec -T postgres sh -ec 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<SQL
INSERT INTO core.accounts (did, handle_cache) VALUES ('did:plc:releasee2e', 'release-e2e')
ON CONFLICT DO NOTHING;
INSERT INTO core.owner_routes (alias, kind, account_did, created_at)
VALUES ('release-e2e', 'account', 'did:plc:releasee2e', now())
ON CONFLICT DO NOTHING;
INSERT INTO core.repositories (
  id, owner_did, slug, display_name, visibility, state, default_branch, storage_key, created_at, updated_at
) VALUES (
  '$release_repository_id', 'did:plc:releasee2e', 'project', 'Release fixture', 'public', 'active', 'main',
  '$release_repository_id', now(), now()
)
ON CONFLICT DO NOTHING;
INSERT INTO auth.access_tokens (
  id, account_did, name, token_prefix, token_hash, scopes, repository_id, created_at
) VALUES (
  '0198aaaa-0000-7000-8000-00000000ee11', 'did:plc:releasee2e', 'release e2e', 'adn_pat_release',
  decode('$release_token_hash', 'hex'), ARRAY['repository:write'], '$release_repository_id', now()
)
ON CONFLICT DO NOTHING;
SQL
    "${compose[@]}" exec -T adenosine sh -ec '
      repository_id="$1"
      compact=$(printf "%s" "$repository_id" | tr -d -)
      repository_path="$ADENOSINE_REPO_ROOT/${compact%${compact#??}}"
      remainder=${compact#??}
      repository_path="$repository_path/${remainder%${remainder#??}}/$repository_id.git"
      if test -f "$repository_path/HEAD"; then
        exit 0
      fi
      mkdir -p "$(dirname "$repository_path")"
      git init --bare "$repository_path" >/dev/null
      work=$(mktemp -d /tmp/adenosine-release-e2e.XXXXXX)
      git -C "$work" init -b main >/dev/null
      git -C "$work" config user.name "Release E2E"
      git -C "$work" config user.email "release-e2e@example.invalid"
      printf "release fixture\n" > "$work/README.md"
      git -C "$work" add README.md
      git -C "$work" commit -m "release fixture" >/dev/null
      git -C "$work" tag v1.0.0
      git -C "$work" tag v2.0.0
      git -C "$work" remote add target "$repository_path"
      git -C "$work" push target main --tags >/dev/null
    ' _ "$release_repository_id"

    release_base="$public_url/api/v1/repositories/release-e2e/project/releases"
    while read -r stale_release_id; do
      test -n "$stale_release_id" || continue
      curl -fsS -o /dev/null -X DELETE -H "Authorization: Bearer $release_token" "$release_base/$stale_release_id"
    done < <(curl -fsS -H "Authorization: Bearer $release_token" "$release_base?limit=100" | jq -r '.items[].id')
    release_response=$(curl -fsS -H "Authorization: Bearer $release_token" -H 'Content-Type: application/json' \
      --data '{"tag_name":"v1.0.0","name":"Version 1","body":"## Changes","draft":false,"prerelease":false}' "$release_base")
    release_id=$(jq -er '.id' <<<"$release_response")
    [[ "$(jq -r '.target_sha | test("^[0-9a-f]{40}$")' <<<"$release_response")" == true ]]
    draft_response=$(curl -fsS -H "Authorization: Bearer $release_token" -H 'Content-Type: application/json' \
      --data '{"tag_name":"v2.0.0","name":"Version 2","body":"private notes","draft":true,"prerelease":true}' "$release_base")
    draft_id=$(jq -er '.id' <<<"$draft_response")
    [[ "$(curl -sS -o /dev/null -w '%{http_code}' "$release_base/$draft_id")" == 404 ]]
    [[ "$(curl -fsS "$release_base" | jq '.items | length')" == 1 ]]

    asset_response=$(curl -fsS -H "Authorization: Bearer $release_token" -H 'Content-Type: application/octet-stream' \
      -H 'X-Asset-Content-Type: text/plain' --data-binary 'release asset bytes' \
      "$release_base/$release_id/assets?name=checksums.txt")
    asset_id=$(jq -er '.id' <<<"$asset_response")
    asset_checksum=$(printf 'release asset bytes' | openssl dgst -sha256 | awk '{print $NF}')
    [[ "$(jq -r '.sha256' <<<"$asset_response")" == "$asset_checksum" ]]
    release_header_file=$(mktemp /tmp/adenosine-release-headers.XXXXXX)
    release_body_file=$(mktemp /tmp/adenosine-release-body.XXXXXX)
    curl -fsS -D "$release_header_file" -o "$release_body_file" "$release_base/$release_id/assets/$asset_id"
    [[ "$(cat "$release_body_file")" == 'release asset bytes' ]]
    tr -d '\r' < "$release_header_file" | grep -qi "^X-Checksum-Sha256: $asset_checksum$"
    tr -d '\r' < "$release_header_file" | grep -qi '^Cache-Control: public, max-age=31536000, immutable$'
    rm -f "$release_header_file" "$release_body_file"
    [[ "$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $release_token" "$release_base/$release_id/assets/$asset_id")" == 204 ]]
    [[ "$(curl -sS -o /dev/null -w '%{http_code}' "$release_base/$release_id/assets/$asset_id")" == 404 ]]
    [[ "$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $release_token" "$release_base/$release_id")" == 204 ]]
    [[ "$(curl -sS -o /dev/null -w '%{http_code}' "$release_base/$release_id")" == 404 ]]
    [[ "$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $release_token" "$release_base/$draft_id")" == 204 ]]
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
        "${federation_compose[@]}" ps --all >&2 || true
        "${federation_compose[@]}" logs --no-color postgres-a postgres-b adenosine-a adenosine-a-tls adenosine-b adenosine-b-tls electric-a electric-b realtime-boundary realtime-sync-gateway realtime-observer >&2 || true
      fi
      "${federation_compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
      trap - EXIT INT TERM
      exit "$status"
    }
    trap cleanup EXIT INT TERM

    wait_for_marker() {
      service=$1
      marker=$2
      for attempt in $(seq 1 60); do
        if "${federation_compose[@]}" exec -T "$service" test -f "$marker"; then
          return 0
        fi
        sleep 0.5
      done
      echo "timed out waiting for $service marker $marker" >&2
      return 1
    }

    "${federation_compose[@]}" build adenosine-a adenosine-b federation-acceptance federation-star federation-issue federation-comment federation-pr realtime-boundary realtime-producer realtime-sync-gateway realtime-observer
    "${federation_compose[@]}" up --detach --wait postgres-a postgres-b adenosine-a adenosine-b adenosine-a-tls adenosine-b-tls electric-a electric-b realtime-boundary realtime-sync-gateway
    "${federation_compose[@]}" exec -T adenosine-a go run ./test/federationhost
    "${federation_compose[@]}" exec -T adenosine-b go run ./test/federationhost -instance=b
    "${federation_compose[@]}" run --rm federation-acceptance federation-acceptance -phase=seed
    "${federation_compose[@]}" run --rm federation-star
    "${federation_compose[@]}" run --rm federation-acceptance federation-acceptance -phase=star
    "${federation_compose[@]}" run --rm federation-issue
    "${federation_compose[@]}" run --rm federation-acceptance federation-acceptance -phase=issue
    "${federation_compose[@]}" run --rm federation-acceptance federation-acceptance -phase=triage
    "${federation_compose[@]}" run --rm federation-comment go run ./test/federationcomment -phase=create
    "${federation_compose[@]}" run --rm federation-acceptance federation-acceptance -phase=comments
    "${federation_compose[@]}" run --rm federation-comment go run ./test/federationcomment -phase=moderate
    "${federation_compose[@]}" run --rm federation-comment go run ./test/federationcomment -phase=delete
    "${federation_compose[@]}" run --rm federation-acceptance federation-acceptance -phase=comments-deleted
    "${federation_compose[@]}" run --rm federation-pr go run ./test/federationpr -phase=create
    "${federation_compose[@]}" exec -T adenosine-a go run ./test/federationpr -phase=fetch
    "${federation_compose[@]}" run --rm federation-pr go run ./test/federationpr -phase=merge
    "${federation_compose[@]}" run --rm federation-acceptance federation-transfer
    "${federation_compose[@]}" run --rm federation-acceptance federation-acceptance -phase=transfer
    "${federation_compose[@]}" up --detach realtime-observer
    wait_for_marker realtime-sync-gateway /tmp/create-live-ready
    "${federation_compose[@]}" run --rm realtime-producer realtime-producer -phase=create
    wait_for_marker realtime-sync-gateway /tmp/delete-live-ready
    "${federation_compose[@]}" stop adenosine-a
    "${federation_compose[@]}" run --rm --no-deps realtime-producer realtime-producer -phase=delete
    observer_id=$("${federation_compose[@]}" ps --all --quiet realtime-observer)
    test -n "$observer_id"
    test "$(docker wait "$observer_id")" = 0
    rest_before=$("${federation_compose[@]}" exec -T adenosine-b wget -qO- "http://localhost:8080/api/v1/network/repositories?limit=1")
    "${federation_compose[@]}" stop electric-b
    rest_after=$("${federation_compose[@]}" exec -T adenosine-b wget -qO- "http://localhost:8080/api/v1/network/repositories?limit=1")
    test "$rest_before" = "$rest_after"
    "${federation_compose[@]}" stop electric-a postgres-a
    "${federation_compose[@]}" run --rm --no-deps federation-pr go run ./test/federationpr -phase=final
    "${federation_compose[@]}" run --rm --no-deps federation-acceptance federation-acceptance -phase=final
    ;;
  *)
    echo "unknown task: $task" >&2
    exit 2
    ;;
esac
