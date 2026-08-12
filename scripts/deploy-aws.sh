#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/deploy-aws.sh [--stack NAME] [--yes] [--skip-conformance]

Requires authenticated aws and pulumi CLIs plus ADENOSINE_DOMAIN, ADENOSINE_IMAGE,
ADENOSINE_ROUTE53_ZONE_ID, ADENOSINE_OAUTH_STATE_KEY, and ADENOSINE_OAUTH_CREDENTIAL_KEY.
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
for command in pulumi aws curl; do command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 1; }; done
aws sts get-caller-identity >/dev/null
for variable in ADENOSINE_DOMAIN ADENOSINE_IMAGE ADENOSINE_ROUTE53_ZONE_ID ADENOSINE_OAUTH_STATE_KEY ADENOSINE_OAUTH_CREDENTIAL_KEY; do [[ -n "${!variable:-}" ]] || { echo "$variable is required" >&2; exit 1; }; done
[[ "$ADENOSINE_IMAGE" =~ @sha256:[a-f0-9]{64}$ ]] || { echo "ADENOSINE_IMAGE must be immutable" >&2; exit 1; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"; project="$root/infra/pulumi/aws"
pulumi -C "$project" stack select "$stack" --create
pulumi -C "$project" config set domain "$ADENOSINE_DOMAIN"
pulumi -C "$project" config set image "$ADENOSINE_IMAGE"
pulumi -C "$project" config set zoneId "$ADENOSINE_ROUTE53_ZONE_ID"
pulumi -C "$project" config set --secret oauthStateKey "$ADENOSINE_OAUTH_STATE_KEY"
pulumi -C "$project" config set --secret oauthCredentialKey "$ADENOSINE_OAUTH_CREDENTIAL_KEY"
pulumi -C "$project" up "${yes[@]}"
health_url="$(pulumi -C "$project" stack output healthUrl)"; curl --retry 30 --retry-delay 10 --retry-all-errors -fsS "$health_url" >/dev/null
if ((conformance)); then
  [[ -n "${ADENOSINE_CONFORMANCE_PAT:-}" && -n "${ADENOSINE_CONFORMANCE_OWNER:-}" && -n "${ADENOSINE_CONFORMANCE_REPOSITORY:-}" ]] || { echo "conformance credentials are not set; use --skip-conformance to acknowledge" >&2; exit 1; }
  ADENOSINE_CONFORMANCE_TARGET=aws ADENOSINE_CONFORMANCE_CAPABILITIES="health,docs,openapi,git-http,web,build-identity,ssh,telemetry,backup-restore" ADENOSINE_CONFORMANCE_IMAGE_DIGEST="${ADENOSINE_IMAGE##*@sha256:}" ADENOSINE_CONFORMANCE_BASE_URL="$(pulumi -C "$project" stack output webUrl)" ADENOSINE_CONFORMANCE_SSH_HOST="$(pulumi -C "$project" stack output sshHost)" ADENOSINE_CONFORMANCE_SSH_PORT="$(pulumi -C "$project" stack output sshPort)" "$root/scripts/conformance.sh" --non-interactive
fi
printf 'Web: %s\nSSH: %s:%s\nDocs: %s\n' "$(pulumi -C "$project" stack output webUrl)" "$(pulumi -C "$project" stack output sshHost)" "$(pulumi -C "$project" stack output sshPort)" "$(pulumi -C "$project" stack output apiDocsUrl)"
