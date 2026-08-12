#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

usage() {
  echo "Usage: scripts/release.sh --version vX.Y.Z [--output DIRECTORY]"
}

version=""
output="$root/dist"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) [[ $# -ge 2 ]] || { echo "--version requires a value" >&2; exit 1; }; version="$2"; shift 2 ;;
    --output) [[ $# -ge 2 ]] || { echo "--output requires a directory" >&2; exit 1; }; output="$2"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] || { echo "--version must be vX.Y.Z" >&2; exit 1; }
[[ -f "$root/docs/releases/$version.md" ]] || { echo "docs/releases/$version.md is required" >&2; exit 1; }

cd "$root"
root_real="$(pwd -P)"
[[ "$(basename "$output")" != "." && "$(basename "$output")" != ".." ]] || { echo "--output must not end in . or .." >&2; exit 1; }
mkdir -p "$(dirname "$output")"
output_parent="$(cd "$(dirname "$output")" && pwd -P)"
output="$output_parent/$(basename "$output")"
[[ "$output" != "/" && "$output" != "$root_real" && "$output" == "$root_real/"* ]] || { echo "--output must be a child directory of $root_real" >&2; exit 1; }
for tool in go tar syft; do command -v "$tool" >/dev/null 2>&1 || { echo "$tool is required" >&2; exit 1; }; done
rm -rf -- "$output"
mkdir -p "$output"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os="${target%/*}"
  arch="${target#*/}"
  name="adenosine_${version#v}_${os}_${arch}"
  work="$(mktemp -d)"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$work/adenosine" ./cmd/adenosine
  tar -czf "$output/$name.tar.gz" -C "$work" adenosine
  rm -rf "$work"
done
cp "$root/api/openapi.yaml" "$output/adenosine-openapi-$version.yaml"
tar -czf "$output/adenosine-api-client-$version.tar.gz" -C "$root/packages/api-client" package.json src
syft dir:"$root" -o spdx-json="$output/adenosine-source-$version.spdx.json" >/dev/null
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$output" && sha256sum ./* > SHA256SUMS)
else
  (cd "$output" && shasum -a 256 ./* > SHA256SUMS)
fi
echo "Release artifacts created in $output"
