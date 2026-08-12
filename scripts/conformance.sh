#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/conformance.sh [--config FILE] [--non-interactive]

Run deployment-neutral health, API documentation, OpenAPI, Git HTTP, optional SSH,
telemetry, and backup/restore conformance. See test/deployment/conformance.env.example.
EOF
}

config_file=""
while (($#)); do
  case "$1" in
    --config) (($# >= 2)) || { echo "--config requires a file" >&2; exit 2; }; config_file="$2"; shift 2 ;;
    --non-interactive) export GIT_TERMINAL_PROMPT=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -n "$config_file" ]]; then
  [[ -f "$config_file" ]] || { echo "config file not found: $config_file" >&2; exit 1; }
  set -a
  # The config is an operator-controlled shell environment file so hooks can be quoted commands.
  # shellcheck disable=SC1090
  source "$config_file"
  set +a
fi

for command in curl git jq mktemp; do command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 1; }; done
required=(ADENOSINE_CONFORMANCE_BASE_URL ADENOSINE_CONFORMANCE_RELEASE ADENOSINE_CONFORMANCE_OWNER ADENOSINE_CONFORMANCE_REPOSITORY ADENOSINE_CONFORMANCE_PAT)
for variable in "${required[@]}"; do [[ -n "${!variable:-}" ]] || { echo "$variable is required" >&2; exit 1; }; done

base_url="${ADENOSINE_CONFORMANCE_BASE_URL%/}"
target="${ADENOSINE_CONFORMANCE_TARGET:-custom}"
capabilities=",${ADENOSINE_CONFORMANCE_CAPABILITIES:-health,docs,openapi,git-http},"
has_capability() { [[ "$capabilities" == *",$1,"* ]]; }
if [[ "$target" != custom ]]; then
  for capability in health docs openapi git-http web build-identity ssh telemetry backup-restore; do
    has_capability "$capability" || { echo "official target $target is missing mandatory capability: $capability" >&2; exit 1; }
  done
fi
[[ "$base_url" =~ ^https?://[^/@]+$ ]] || { echo "ADENOSINE_CONFORMANCE_BASE_URL must be an origin without credentials or path" >&2; exit 1; }
[[ "$ADENOSINE_CONFORMANCE_OWNER" =~ ^[A-Za-z0-9._-]+$ ]] || { echo "invalid owner" >&2; exit 1; }
[[ "$ADENOSINE_CONFORMANCE_REPOSITORY" =~ ^[A-Za-z0-9._-]+$ ]] || { echo "invalid repository" >&2; exit 1; }

work="$(mktemp -d "${TMPDIR:-/tmp}/adenosine-conformance.XXXXXX")"
askpass="$work/askpass.sh"
branch="conformance-$(date +%s)-$$"
tag="$branch"
backup_branch="$branch-backup"
remote_path="$ADENOSINE_CONFORMANCE_OWNER/$ADENOSINE_CONFORMANCE_REPOSITORY.git"
export ADENOSINE_CONFORMANCE_PAT
cat >"$askpass" <<'EOF'
#!/bin/sh
case "$1" in
  *Username*) printf '%s\n' adenosine ;;
  *Password*) printf '%s\n' "$ADENOSINE_CONFORMANCE_PAT" ;;
  *) exit 1 ;;
esac
EOF
chmod 700 "$askpass"

git_auth() { GIT_ASKPASS="$askpass" GIT_TERMINAL_PROMPT=0 git -c credential.helper= "$@"; }
cleanup() {
  set +e
  if [[ -d "$work/http/.git" ]]; then
    git_auth -C "$work/http" push origin ":refs/heads/$branch" ":refs/tags/$tag" >/dev/null 2>&1
    git_auth -C "$work/http" push origin ":refs/heads/$backup_branch" >/dev/null 2>&1
  fi
  unset ADENOSINE_CONFORMANCE_PAT
  rm -rf -- "$work"
}
trap cleanup EXIT INT TERM

ok() { printf 'ok: %s\n' "$1"; }
skip() { printf 'skip: %s\n' "$1"; }

curl -fsS "$base_url/health/live" >/dev/null && ok "liveness"
curl -fsS "$base_url/health/ready" >/dev/null && ok "readiness"
curl -fsS "$base_url/" | grep -q 'Build in public' && ok "public web application"
curl -fsS "$base_url/docs/api" | grep -q 'api-reference' && ok "API documentation"
openapi="$work/openapi.json"
curl -fsS "$base_url/openapi.json" -o "$openapi"
[[ "$(jq -r '.openapi' "$openapi")" == "3.0.3" ]] || { echo "unexpected OpenAPI specification version" >&2; exit 1; }
[[ "$(jq -r '.info.version' "$openapi")" == "$ADENOSINE_CONFORMANCE_RELEASE" ]] || { echo "OpenAPI release does not match ADENOSINE_CONFORMANCE_RELEASE" >&2; exit 1; }
ok "OpenAPI contract"
if has_capability build-identity; then
  expected_digest="${ADENOSINE_CONFORMANCE_IMAGE_DIGEST:-}"
  [[ "$expected_digest" =~ ^[a-f0-9]{64}$ ]] || { echo "build-identity capability requires ADENOSINE_CONFORMANCE_IMAGE_DIGEST" >&2; exit 1; }
  identity_headers="$work/build-identity-headers"
  curl -fsS -D "$identity_headers" -o /dev/null "$base_url/health/live"
  actual_digest="$(tr -d '\r' <"$identity_headers" | awk 'BEGIN { IGNORECASE=1 } /^X-Adenosine-Image-Digest:/ { print $2 }')"
  [[ "$actual_digest" == "$expected_digest" ]] || { echo "deployed immutable image identity does not match" >&2; exit 1; }
  ok "immutable build identity"
fi

git clone "$base_url/$remote_path" "$work/http"
git -C "$work/http" config user.name "Adenosine Conformance"
git -C "$work/http" config user.email "conformance@invalid.example"
printf 'deployment conformance %s\n' "$branch" >"$work/http/$branch.txt"
git -C "$work/http" add "$branch.txt"
git -C "$work/http" commit -m "test deployment conformance"
git_auth -C "$work/http" push origin "HEAD:refs/heads/$branch"
git -C "$work/http" tag "$tag"
git_auth -C "$work/http" push origin "refs/tags/$tag:refs/tags/$tag"
git -C "$work/http" fetch --prune origin
git_auth -C "$work/http" push origin ":refs/heads/$branch" ":refs/tags/$tag"
ok "Git Smart HTTP clone/fetch/push/tag/ref deletion"

ssh_values=("${ADENOSINE_CONFORMANCE_SSH_HOST:-}" "${ADENOSINE_CONFORMANCE_SSH_KEY:-}" "${ADENOSINE_CONFORMANCE_SSH_KNOWN_HOSTS:-}")
if [[ -n "${ssh_values[0]}" || -n "${ssh_values[1]}" || -n "${ssh_values[2]}" ]]; then
  [[ -n "${ssh_values[0]}" && -f "${ssh_values[1]}" && -f "${ssh_values[2]}" ]] || { echo "SSH host, key, and known-hosts file must be configured together" >&2; exit 1; }
  command -v ssh >/dev/null 2>&1 || { echo "ssh is required when SSH conformance is enabled" >&2; exit 1; }
  ssh_port="${ADENOSINE_CONFORMANCE_SSH_PORT:-2222}"
  ssh_command="ssh -i ${ssh_values[1]} -o IdentitiesOnly=yes -o UserKnownHostsFile=${ssh_values[2]} -o StrictHostKeyChecking=yes -p $ssh_port"
  GIT_SSH_COMMAND="$ssh_command" git clone "ssh://git@${ssh_values[0]}:$ssh_port/$remote_path" "$work/ssh"
  git -C "$work/ssh" config user.name "Adenosine Conformance"
  git -C "$work/ssh" config user.email "conformance@invalid.example"
  printf 'SSH deployment conformance %s\n' "$branch" >"$work/ssh/$branch-ssh.txt"
  git -C "$work/ssh" add "$branch-ssh.txt"
  git -C "$work/ssh" commit -m "test SSH deployment conformance"
  GIT_SSH_COMMAND="$ssh_command" git -C "$work/ssh" push origin "HEAD:refs/heads/$branch-ssh"
  GIT_SSH_COMMAND="$ssh_command" git -C "$work/ssh" fetch --prune origin
  GIT_SSH_COMMAND="$ssh_command" git -C "$work/ssh" push origin ":refs/heads/$branch-ssh"
  ok "Git SSH clone/fetch/push/ref deletion with pinned host identity"
else
  has_capability ssh && { echo "SSH capability declared but endpoint material is missing" >&2; exit 1; }
  skip "Git SSH (capability not declared)"
fi

if [[ -n "${ADENOSINE_CONFORMANCE_TELEMETRY_COMMAND:-}" ]]; then
  bash -euo pipefail -c "$ADENOSINE_CONFORMANCE_TELEMETRY_COMMAND" && ok "telemetry export"
else
  has_capability telemetry && { echo "telemetry capability declared but hook is missing" >&2; exit 1; }
  skip "telemetry (capability not declared)"
fi

if [[ -n "${ADENOSINE_CONFORMANCE_BACKUP_COMMAND:-}" || -n "${ADENOSINE_CONFORMANCE_RESTORE_COMMAND:-}" ]]; then
  [[ -n "${ADENOSINE_CONFORMANCE_BACKUP_COMMAND:-}" && -n "${ADENOSINE_CONFORMANCE_RESTORE_COMMAND:-}" ]] || { echo "backup and restore hooks must be configured together" >&2; exit 1; }
  backup_id_file="$work/backup-id"
  fixture_sha="$(git -C "$work/http" rev-parse HEAD)"
  git_auth -C "$work/http" push origin "HEAD:refs/heads/$backup_branch"
  export BACKUP_ID_FILE="$backup_id_file"
  bash -euo pipefail -c "$ADENOSINE_CONFORMANCE_BACKUP_COMMAND"
  [[ -s "$backup_id_file" ]] || { echo "backup hook did not write BACKUP_ID_FILE" >&2; exit 1; }
  export BACKUP_ID="$(<"$backup_id_file")"
  bash -euo pipefail -c "$ADENOSINE_CONFORMANCE_RESTORE_COMMAND"
  curl -fsS "$base_url/health/ready" >/dev/null
  git clone "$base_url/$remote_path" "$work/restored"
  restored_sha="$(git -C "$work/restored" rev-parse "origin/$backup_branch")"
  [[ "$restored_sha" == "$fixture_sha" ]] || { echo "backup/restore did not preserve the Git fixture" >&2; exit 1; }
  ok "portable backup and clean restore hook"
else
  has_capability backup-restore && { echo "backup-restore capability declared but hooks are missing" >&2; exit 1; }
  skip "backup/restore (capability not declared)"
fi
