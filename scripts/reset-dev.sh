#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [[ "${1:-}" != "--yes" ]]; then
  read -r -p "Delete local PostgreSQL and repository data? [y/N] " answer
  [[ "$answer" == "y" || "$answer" == "Y" ]] || exit 0
fi

test -f .env.local || { echo "Nothing to reset."; exit 0; }
docker compose --env-file .env.local -f dev/docker-compose.yml down --volumes --remove-orphans
echo "Local development data deleted. Run make dev to recreate it."
