#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ADENOSINE_ENV_FILE:-$ROOT/deploy/.env}"
COMPOSE_FILE="$ROOT/deploy/docker-compose.yml"
MAINTENANCE_FILE="${ADENOSINE_MAINTENANCE_FILE:-$ROOT/deploy/.maintenance}"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

load_env() {
  [[ -f "$ENV_FILE" ]] || die "$ENV_FILE is missing; run scripts/bootstrap.sh"
  local line key value seen=" "
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ -z "$line" || "$line" == \#* ]] && continue
    [[ "$line" == *=* ]] || die "$ENV_FILE contains an invalid line"
    key="${line%%=*}"
    value="${line#*=}"
    [[ "$key" =~ ^[A-Z][A-Z0-9_]*$ ]] || die "$ENV_FILE contains invalid key '$key'"
    [[ "$seen" != *" $key "* ]] || die "$ENV_FILE contains duplicate key '$key'"
    seen+="$key "
    export "$key=$value"
  done < "$ENV_FILE"
}

require_env() {
  local name value
  for name in "$@"; do
    value="${!name:-}"
    [[ -n "$value" && "$value" != "GENERATED" ]] || die "$name must be set in $ENV_FILE"
  done
}

validate_environment() {
  require_env ADENOSINE_DOMAIN ADENOSINE_BASE_URL ADENOSINE_SSH_HOST ADENOSINE_SSH_PORT \
    ADENOSINE_IMAGE ADENOSINE_VERSION POSTGRES_USER POSTGRES_DB POSTGRES_PASSWORD \
    ADENOSINE_OAUTH_STATE_KEY ADENOSINE_OAUTH_CREDENTIAL_KEY TAP_ADMIN_PASSWORD \
    ELECTRIC_DATABASE_PASSWORD ELECTRIC_SECRET
  [[ "$ADENOSINE_DOMAIN" =~ ^[A-Za-z0-9.-]+$ ]] || die "ADENOSINE_DOMAIN is invalid"
  [[ "$ADENOSINE_SSH_HOST" =~ ^[A-Za-z0-9.:-]+$ ]] || die "ADENOSINE_SSH_HOST is invalid"
  [[ "$ADENOSINE_SSH_PORT" =~ ^[0-9]+$ ]] || die "ADENOSINE_SSH_PORT must be numeric"
  [[ "$ADENOSINE_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] || die "ADENOSINE_VERSION must be an immutable vX.Y.Z release"
  [[ "$ADENOSINE_BASE_URL" == "https://$ADENOSINE_DOMAIN" ]] || die "ADENOSINE_BASE_URL must be https://ADENOSINE_DOMAIN"
  [[ "$ADENOSINE_ELECTRIC_URL" == "" || "$ADENOSINE_ELECTRIC_URL" == "http://electric:3000" ]] || die "ADENOSINE_ELECTRIC_URL must be empty or http://electric:3000"
  [[ "$ADENOSINE_ELECTRIC_SECRET" == "" || "$ADENOSINE_ELECTRIC_SECRET" == "$ELECTRIC_SECRET" ]] || die "ADENOSINE_ELECTRIC_SECRET must match ELECTRIC_SECRET"
  case "${ADENOSINE_RELEASE_ASSET_BACKEND:-filesystem}" in
    filesystem)
      for name in ADENOSINE_RELEASE_ASSET_S3_ENDPOINT ADENOSINE_RELEASE_ASSET_S3_REGION \
        ADENOSINE_RELEASE_ASSET_S3_BUCKET ADENOSINE_RELEASE_ASSET_S3_ACCESS_KEY_ID \
        ADENOSINE_RELEASE_ASSET_S3_SECRET_ACCESS_KEY ADENOSINE_RELEASE_ASSET_S3_SESSION_TOKEN; do
        [[ -z "${!name:-}" ]] || die "$name requires ADENOSINE_RELEASE_ASSET_BACKEND=s3"
      done
      [[ "${ADENOSINE_RELEASE_ASSET_S3_PATH_STYLE:-false}" == false ]] || die "ADENOSINE_RELEASE_ASSET_S3_PATH_STYLE requires ADENOSINE_RELEASE_ASSET_BACKEND=s3"
      ;;
    s3)
      require_env ADENOSINE_RELEASE_ASSET_S3_ENDPOINT ADENOSINE_RELEASE_ASSET_S3_REGION \
        ADENOSINE_RELEASE_ASSET_S3_BUCKET ADENOSINE_RELEASE_ASSET_S3_ACCESS_KEY_ID \
        ADENOSINE_RELEASE_ASSET_S3_SECRET_ACCESS_KEY
      [[ "$ADENOSINE_RELEASE_ASSET_S3_ENDPOINT" =~ ^https?://[^/?#]+(/[^?#]*)?$ ]] || die "ADENOSINE_RELEASE_ASSET_S3_ENDPOINT must be an absolute HTTP or HTTPS URL"
      [[ "${ADENOSINE_RELEASE_ASSET_S3_PATH_STYLE:-false}" == true || "${ADENOSINE_RELEASE_ASSET_S3_PATH_STYLE:-false}" == false ]] || die "ADENOSINE_RELEASE_ASSET_S3_PATH_STYLE must be true or false"
      ;;
    *) die "ADENOSINE_RELEASE_ASSET_BACKEND must be filesystem or s3" ;;
  esac
  if [[ ",${COMPOSE_PROFILES:-}," == *,electric,* ]]; then
    [[ "$ADENOSINE_ELECTRIC_URL" == "http://electric:3000" && "$ADENOSINE_ELECTRIC_SECRET" == "$ELECTRIC_SECRET" ]] || die "the electric profile requires Electric application settings"
  elif [[ -n "$ADENOSINE_ELECTRIC_URL" ]]; then
    die "ADENOSINE_ELECTRIC_URL requires the electric Compose profile"
  fi
  if [[ -n "${ADENOSINE_TAP_CONSUMER:-}" ]]; then
    [[ "$ADENOSINE_TAP_CONSUMER" =~ ^tap:[A-Za-z0-9._:-]+$ ]] || die "ADENOSINE_TAP_CONSUMER is invalid"
    [[ ",${COMPOSE_PROFILES:-}," == *,tap,* ]] || die "ADENOSINE_TAP_CONSUMER requires the tap Compose profile"
  elif [[ ",${COMPOSE_PROFILES:-}," == *,tap,* ]]; then
    die "the tap profile requires ADENOSINE_TAP_CONSUMER"
  fi
}

require_portable_release_asset_backend() {
  [[ "${ADENOSINE_RELEASE_ASSET_BACKEND:-filesystem}" == filesystem ]] ||
    die "bundled backup and restore require filesystem release assets; coordinate PostgreSQL and bucket recovery with the S3 provider procedure"
}

need_tools() {
  local tool
  for tool in "$@"; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
}

compose() {
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
}

maintenance_active() {
  [[ "${ADENOSINE_MAINTENANCE:-}" == "1" || -f "$MAINTENANCE_FILE" ]]
}

enter_maintenance() {
  if maintenance_active; then
    return 1
  fi
  mkdir -p "$(dirname "$MAINTENANCE_FILE")"
  (umask 077; printf '%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$MAINTENANCE_FILE")
  export ADENOSINE_MAINTENANCE=1
  if ! compose stop caddy web tap electric app >/dev/null; then
    rm -f "$MAINTENANCE_FILE"
    unset ADENOSINE_MAINTENANCE
    return 1
  fi
  return 0
}

leave_maintenance() {
  compose up -d
  rm -f "$MAINTENANCE_FILE"
  unset ADENOSINE_MAINTENANCE
}

pull_service_images() {
  if [[ "${ADENOSINE_SKIP_PULL:-}" != "1" ]]; then
    compose pull "$@"
  fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}

random_hex() {
  openssl rand -hex "$1"
}

random_base64_32() {
  openssl rand -base64 32 | tr -d '\n'
}
