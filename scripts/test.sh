#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

command -v go >/dev/null 2>&1 || { echo "Go is required." >&2; exit 1; }
command -v bun >/dev/null 2>&1 || { echo "Bun is required." >&2; exit 1; }

go test ./...
bun run --cwd web test
