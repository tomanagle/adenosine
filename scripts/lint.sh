#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

command -v go >/dev/null 2>&1 || { echo "Go is required." >&2; exit 1; }
command -v bun >/dev/null 2>&1 || { echo "Bun is required." >&2; exit 1; }

unformatted="$(find . -name '*.go' -not -path './.air/*' -exec gofmt -l {} +)"
if [[ -n "$unformatted" ]]; then
  echo "Go files need formatting:" >&2
  echo "$unformatted" >&2
  exit 1
fi

go vet ./...
go test ./test/convention
bun run lint
bun run format:check
bun run --cwd web typecheck

panic_uses="$(find . -name '*.go' -not -path './.air/*' -exec grep -HnF 'panic(' {} + | grep -Ev '^./internal/(config/config|di/providers|database/migration/migration|gitssh/host_key|atproto/client|repository/endpoints|passkey/service|syncproxy/proxy)\.go:' || true)"
if [[ -n "$panic_uses" ]]; then
  echo "panic is only allowed in startup Must functions:" >&2
  echo "$panic_uses" >&2
  exit 1
fi
