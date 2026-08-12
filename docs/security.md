# Security Model

Adenosine's public-alpha security posture is defined by the [threat model](security/threat-model.md) and [control checklist](security/public-alpha-checklist.md). Security boundaries are enforced server-side; the web UI, Git author strings, handles, and arbitrary ATProto record authors are not authorization principals. Local repository visibility, collaborator state, credential scope, and account DID control access.

## Credential rules

- Browser sessions and PATs are stored as SHA-256 hashes; PAT plaintext is returned once.
- OAuth credentials are encrypted with the configured external key. OAuth states and passkey ceremonies expire.
- SSH private keys are never accepted or stored. Public keys are parsed, canonicalized, fingerprinted, revocable, and tied to an account DID.
- Session-only credential administration prevents a repository-scoped PAT from minting stronger credentials.
- Cookie-authenticated mutations require the configured same-origin `Origin`.

`auth.*`, `.env.local`, database dumps, encryption keys, Tap/Electric secrets, telemetry exporter headers, and the SSH host private key are sensitive. Do not put them in logs, traces, issues, federation records, or support bundles.

## Operational posture

Run as an unprivileged service account; keep PostgreSQL and profiling endpoints private; terminate TLS at a maintained proxy; set filesystem quotas; restrict repository and state directory permissions; rotate secrets; and test encrypted backups and restores. Do not expose the development Compose stack as production.

The alpha does not claim webhook delivery, staged repository deletion, comprehensive per-principal auth rate limiting, private federation, advanced branch protection, CI runners, LFS, enterprise SSO, or relay/custom-PDS support. See the checklist for compensating operator controls and explicit residual risks. Report vulnerabilities using [`../SECURITY.md`](../SECURITY.md), not a public issue.
