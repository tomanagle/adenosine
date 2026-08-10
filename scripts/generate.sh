#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

command -v go >/dev/null 2>&1 || { echo "Go is required." >&2; exit 1; }
command -v bun >/dev/null 2>&1 || { echo "Bun is required." >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "curl is required." >&2; exit 1; }

sqlc_version="1.31.1"
platform="$(uname -s | tr '[:upper:]' '[:lower:]')"
architecture="$(uname -m)"
case "$architecture" in
  x86_64) architecture="amd64" ;;
  arm64 | aarch64) architecture="arm64" ;;
esac
case "${platform}_${architecture}" in
  darwin_amd64) sqlc_checksum="c5af76772e3785d21663a62697056b383f07629979b1bd25b93872e73dbd519b" ;;
  darwin_arm64) sqlc_checksum="21602158c99eb1f2bae197a66abfb1941d1e9e50b23125bb193349c6b1acc71e" ;;
  linux_amd64) sqlc_checksum="497ae4fcdfa64c5b0c311ffe4c2bd991e43991e82e5367792ed78bc2dca27354" ;;
  linux_arm64) sqlc_checksum="b7cae247740d0c51a1e657479e5b2d21e6fef428f596682a01bc55bf4ab8a23d" ;;
  *) echo "sqlc ${sqlc_version} is not configured for ${platform}/${architecture}." >&2; exit 1 ;;
esac

tools_dir="${XDG_CACHE_HOME:-$HOME/.cache}/adenosine/tools"
sqlc="$tools_dir/sqlc-${sqlc_version}-${platform}-${architecture}"
if [[ ! -x "$sqlc" ]]; then
  command -v tar >/dev/null 2>&1 || { echo "tar is required." >&2; exit 1; }
  work_dir="$(mktemp -d)"
  trap 'rm -rf "$work_dir"' EXIT
  archive="$work_dir/sqlc.tar.gz"
  curl -fsSL "https://github.com/sqlc-dev/sqlc/releases/download/v${sqlc_version}/sqlc_${sqlc_version}_${platform}_${architecture}.tar.gz" -o "$archive"
  if command -v sha256sum >/dev/null 2>&1; then
    actual_checksum="$(sha256sum "$archive" | cut -d' ' -f1)"
  else
    actual_checksum="$(shasum -a 256 "$archive" | cut -d' ' -f1)"
  fi
  if [[ "$actual_checksum" != "$sqlc_checksum" ]]; then
    echo "sqlc ${sqlc_version} checksum mismatch." >&2
    exit 1
  fi
  mkdir -p "$tools_dir"
  tar -xzf "$archive" -C "$work_dir" sqlc
  mv "$work_dir/sqlc" "$sqlc"
  chmod 755 "$sqlc"
fi

"$sqlc" generate
GOTOOLCHAIN=local go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.7.2 --config api/oapi-codegen.yaml api/openapi.yaml
bun x openapi-ts --file packages/api-client/openapi-ts.config.ts
gofmt -w api/generated/go internal/database/generated
