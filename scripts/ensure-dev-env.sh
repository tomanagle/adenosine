#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

umask 077
oauth_credential_key="$(openssl rand -base64 32)"
tap_admin_password="$(openssl rand -hex 32 2>/dev/null || od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
electric_secret="$(openssl rand -hex 32 2>/dev/null || od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
electric_database_password="$(openssl rand -hex 24 2>/dev/null || od -An -N24 -tx1 /dev/urandom | tr -d ' \n')"
if [[ -f .env.local ]]; then
  if ! grep -q '^ADENOSINE_OAUTH_CREDENTIAL_KEY=' .env.local; then
    printf 'ADENOSINE_OAUTH_CREDENTIAL_KEY=%s\n' "$oauth_credential_key" >> .env.local
    echo "Added ADENOSINE_OAUTH_CREDENTIAL_KEY to .env.local."
  fi
  if ! grep -q '^ADENOSINE_TAP_CONSUMER=' .env.local; then
    printf 'ADENOSINE_TAP_CONSUMER=tap:dev.adenosine:v1\n' >> .env.local
    echo "Enabled the Tap consumer in .env.local."
  fi
  if ! grep -q '^TAP_ADMIN_PASSWORD=' .env.local; then
    printf 'TAP_ADMIN_PASSWORD=%s\n' "$tap_admin_password" >> .env.local
    echo "Added TAP_ADMIN_PASSWORD to .env.local."
  fi
  if ! grep -q '^ADENOSINE_TAP_ADMIN_PASSWORD=' .env.local; then
    printf 'ADENOSINE_TAP_ADMIN_PASSWORD=%s\n' "$(grep '^TAP_ADMIN_PASSWORD=' .env.local | cut -d= -f2-)" >> .env.local
    echo "Added ADENOSINE_TAP_ADMIN_PASSWORD to .env.local."
  fi
  if ! grep -q '^ELECTRIC_SECRET=' .env.local; then
    printf 'ELECTRIC_SECRET=%s\n' "$electric_secret" >> .env.local
    echo "Added ELECTRIC_SECRET to .env.local."
  fi
  if ! grep -q '^ELECTRIC_DATABASE_PASSWORD=' .env.local; then
    printf 'ELECTRIC_DATABASE_PASSWORD=%s\n' "$electric_database_password" >> .env.local
    echo "Added ELECTRIC_DATABASE_PASSWORD to .env.local."
  fi
  exit 0
fi

password="$(openssl rand -hex 24 2>/dev/null || od -An -N24 -tx1 /dev/urandom | tr -d ' \n')"
oauth_state_key="$(openssl rand -base64 32)"
{
  printf 'POSTGRES_USER=adenosine\n'
  printf 'POSTGRES_DB=adenosine\n'
  printf 'POSTGRES_PASSWORD=%s\n' "$password"
  printf 'POSTGRES_PORT=5432\n'
  printf 'DATABASE_URL=postgres://adenosine:%s@postgres:5432/adenosine?sslmode=disable\n' "$password"
  printf 'ADENOSINE_BASE_URL=http://127.0.0.1:8080\n'
  printf 'ADENOSINE_LISTEN_ADDR=:8080\n'
  printf 'ADENOSINE_REPO_ROOT=/var/lib/adenosine/repos\n'
  printf 'ADENOSINE_RELEASE_ASSET_ROOT=/var/lib/adenosine/state/release-assets\n'
  printf 'ADENOSINE_GIT_BINARY=git\n'
  printf 'ADENOSINE_HTTP_PORT=8080\n'
  printf 'ADENOSINE_SSH_LISTEN_ADDR=:2222\n'
  printf 'ADENOSINE_SSH_HOST_KEY_PATH=/var/lib/adenosine/state/ssh_host_ed25519_key\n'
  printf 'ADENOSINE_SSH_HOST=localhost\n'
  printf 'ADENOSINE_SSH_PORT=2222\n'
  printf 'ADENOSINE_OAUTH_STATE_KEY=%s\n' "$oauth_state_key"
  printf 'ADENOSINE_OAUTH_CREDENTIAL_KEY=%s\n' "$oauth_credential_key"
  printf 'ADENOSINE_TAP_CONSUMER=tap:dev.adenosine:v1\n'
  printf 'ADENOSINE_TAP_ADMIN_PASSWORD=%s\n' "$tap_admin_password"
  printf 'TAP_ADMIN_PASSWORD=%s\n' "$tap_admin_password"
  printf 'ELECTRIC_SECRET=%s\n' "$electric_secret"
  printf 'ELECTRIC_DATABASE_PASSWORD=%s\n' "$electric_database_password"
  printf 'GRAFANA_PORT=3001\n'
  printf 'OTEL_GRPC_PORT=4317\n'
  printf 'OTEL_HTTP_PORT=4318\n'
} > .env.local
echo "Created .env.local with development credentials."
