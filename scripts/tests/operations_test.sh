#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
env_file="$temporary/production.env"

"$root/scripts/bootstrap.sh" --env-file "$env_file" --domain code.test --version v1.2.3 --env-only >/dev/null
[[ "$(stat -f %Lp "$env_file" 2>/dev/null || stat -c %a "$env_file")" == 600 ]]
before="$(shasum -a 256 "$env_file")"
"$root/scripts/bootstrap.sh" --env-file "$env_file" --env-only >/dev/null
after="$(shasum -a 256 "$env_file")"
[[ "$before" == "$after" ]]
grep -q '^ADENOSINE_BASE_URL=https://code.test$' "$env_file"
! grep -q '=GENERATED$' "$env_file"

duplicate_env="$temporary/duplicate.env"
cp "$env_file" "$duplicate_env"
printf 'POSTGRES_DB=duplicate\n' >> "$duplicate_env"
if "$root/scripts/bootstrap.sh" --env-file "$duplicate_env" --env-only >/dev/null 2>&1; then
  echo "duplicate environment keys were accepted" >&2
  exit 1
fi

for script in bootstrap migrate doctor backup restore upgrade rollback release; do
  "$root/scripts/$script.sh" --help >/dev/null
done

if "$root/scripts/release.sh" --version v0.1.0 --output "$root" >"$temporary/release.out" 2>&1; then
  echo "release accepted the repository root as output" >&2
  exit 1
fi
grep -q -- '--output must be a child directory' "$temporary/release.out"

printf 'ok: production operation script tests\n'
