#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/deploy-railway.sh [--stack NAME] [--yes] [--skip-conformance]

Required environment: RAILWAY_API_TOKEN, ADENOSINE_DOMAIN, ADENOSINE_IMAGE,
ADENOSINE_OAUTH_STATE_KEY, ADENOSINE_OAUTH_CREDENTIAL_KEY.
ADENOSINE_IMAGE must use name@sha256:digest. Secrets are written with pulumi config set --secret.
EOF
}
stack="${PULUMI_STACK:-production}"; yes=(); conformance=1
while (($#)); do case "$1" in
  --stack) (($# >= 2)) || { echo "--stack requires a name" >&2; exit 2; }; stack="$2"; shift 2 ;;
  --yes) yes=(--yes); shift ;;
  --skip-conformance) conformance=0; shift ;;
  -h|--help) usage; exit 0 ;;
  *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
esac; done
for command in pulumi railway curl; do command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 1; }; done
for variable in RAILWAY_API_TOKEN ADENOSINE_DOMAIN ADENOSINE_IMAGE ADENOSINE_OAUTH_STATE_KEY ADENOSINE_OAUTH_CREDENTIAL_KEY; do [[ -n "${!variable:-}" ]] || { echo "$variable is required" >&2; exit 1; }; done
[[ "$ADENOSINE_IMAGE" =~ @sha256:[a-f0-9]{64}$ ]] || { echo "ADENOSINE_IMAGE must be immutable" >&2; exit 1; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"; project="$root/infra/pulumi/railway"
pulumi -C "$project" stack select "$stack" --create
pulumi -C "$project" config set domain "$ADENOSINE_DOMAIN"
pulumi -C "$project" config set image "$ADENOSINE_IMAGE"
pulumi -C "$project" config set --secret oauthStateKey "$ADENOSINE_OAUTH_STATE_KEY"
pulumi -C "$project" config set --secret oauthCredentialKey "$ADENOSINE_OAUTH_CREDENTIAL_KEY"
pulumi -C "$project" up "${yes[@]}"
project_id="$(pulumi -C "$project" stack output projectId)"
environment_id="$(pulumi -C "$project" stack output environmentId)"
health_url="$(pulumi -C "$project" stack output healthUrl)"
curl --retry 20 --retry-delay 5 --retry-all-errors -fsS "$health_url" >/dev/null
if ((conformance)); then
  [[ -n "${ADENOSINE_CONFORMANCE_PAT:-}" && -n "${ADENOSINE_CONFORMANCE_OWNER:-}" && -n "${ADENOSINE_CONFORMANCE_REPOSITORY:-}" ]] || { echo "conformance credentials are not set; use --skip-conformance to acknowledge" >&2; exit 1; }
  ADENOSINE_CONFORMANCE_TARGET=railway ADENOSINE_CONFORMANCE_CAPABILITIES="health,docs,openapi,git-http,web,build-identity,${ADENOSINE_CONFORMANCE_RAILWAY_CAPABILITIES:-}" ADENOSINE_CONFORMANCE_IMAGE_DIGEST="${ADENOSINE_IMAGE##*@sha256:}" ADENOSINE_CONFORMANCE_BASE_URL="$(pulumi -C "$project" stack output webUrl)" "$root/scripts/conformance.sh" --non-interactive
fi
printf 'Web: %s\nDocs: %s\nHealth: %s\n' "$(pulumi -C "$project" stack output webUrl)" "$(pulumi -C "$project" stack output apiDocsUrl)" "$health_url"
