#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../deploy/lib.sh
source "$root/deploy/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/bootstrap.sh [--env-file PATH] [--domain HOST] [--version vX.Y.Z] [--env-only]

Create or complete the production environment without replacing existing values. Unless
--env-only is used, also create durable volumes and the SSH host key.
EOF
}

domain=""
version=""
env_only=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --env-file) [[ $# -ge 2 ]] || die "--env-file requires a path"; ENV_FILE="$2"; shift 2 ;;
    --domain) [[ $# -ge 2 ]] || die "--domain requires a host"; domain="$2"; shift 2 ;;
    --version) [[ $# -ge 2 ]] || die "--version requires a value"; version="$2"; shift 2 ;;
    --env-only) env_only=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

need_tools openssl
if [[ ! -f "$ENV_FILE" ]]; then
  [[ -n "$domain" ]] || die "--domain is required when creating $ENV_FILE"
  [[ "$domain" =~ ^[A-Za-z0-9.-]+$ ]] || die "--domain is invalid"
  mkdir -p "$(dirname "$ENV_FILE")"
  umask 077
  while IFS= read -r line || [[ -n "$line" ]]; do
    case "$line" in
      ADENOSINE_DOMAIN=*) printf 'ADENOSINE_DOMAIN=%s\n' "$domain" ;;
      ADENOSINE_BASE_URL=*) printf 'ADENOSINE_BASE_URL=https://%s\n' "$domain" ;;
      ADENOSINE_SSH_HOST=*) printf 'ADENOSINE_SSH_HOST=%s\n' "$domain" ;;
      ADENOSINE_VERSION=*) printf 'ADENOSINE_VERSION=%s\n' "${version:-v0.1.0}" ;;
      *) printf '%s\n' "$line" ;;
    esac
  done < "$root/deploy/.env.example" > "$ENV_FILE"
elif [[ -n "$domain" || -n "$version" ]]; then
  die "--domain and --version never replace values in an existing environment"
fi

replace_generated() {
  local key="$1" value="$2" line key_present=0 temporary
  temporary="$(mktemp "${ENV_FILE}.XXXXXX")"
  chmod 600 "$temporary"
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" == "$key="* ]]; then
      key_present=1
    fi
    if [[ "$line" == "$key=GENERATED" ]]; then
      printf '%s=%s\n' "$key" "$value"
    else
      printf '%s\n' "$line"
    fi
  done < "$ENV_FILE" > "$temporary"
  if [[ $key_present -eq 0 ]]; then
    printf '%s=%s\n' "$key" "$value" >> "$temporary"
  fi
  mv "$temporary" "$ENV_FILE"
}

replace_generated POSTGRES_PASSWORD "$(random_hex 32)"
replace_generated ADENOSINE_OAUTH_STATE_KEY "$(random_base64_32)"
replace_generated ADENOSINE_OAUTH_CREDENTIAL_KEY "$(random_base64_32)"
replace_generated TAP_ADMIN_PASSWORD "$(random_hex 32)"
replace_generated ELECTRIC_DATABASE_PASSWORD "$(random_hex 32)"
replace_generated ELECTRIC_SECRET "$(random_hex 32)"
chmod 600 "$ENV_FILE"
load_env
validate_environment

if [[ $env_only -eq 0 ]]; then
  need_tools docker
  docker info >/dev/null 2>&1 || die "Docker is not running"
  pull_service_images app
  compose run --rm --no-deps --user root --entrypoint sh app -ec '
    mkdir -p /var/lib/adenosine/repos /var/lib/adenosine/state
    chown -R adenosine:adenosine /var/lib/adenosine/repos /var/lib/adenosine/state
    key=/var/lib/adenosine/state/ssh_host_ed25519_key
    if [ ! -f "$key" ]; then ssh-keygen -q -t ed25519 -N "" -f "$key"; fi
    chown adenosine:adenosine "$key" "$key.pub"
    chmod 600 "$key"
  '
fi

printf 'Production environment initialized at %s. Secrets were not displayed.\n' "$ENV_FILE"
