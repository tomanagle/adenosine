# Security Model

Security boundaries are enforced server-side; the web UI is not privileged. Local
repository visibility and collaborator state authorize Git and REST access. DIDs identify
accounts, but untrusted Git author strings, handles, and arbitrary ATProto record authors
do not grant permissions.

## Credentials

- Browser sessions and PATs are stored only as SHA-256 hashes; PAT plaintext is returned once.
- OAuth credentials are encrypted with the configured key; state and ceremonies are bounded and expiring.
- SSH private keys are never accepted or stored. Public keys are canonicalized and fingerprinted.
- Session-only credential administration prevents a repository-scoped PAT from minting stronger credentials.
- Cookie-authenticated mutations require the configured same-origin `Origin` value.

`auth.*`, `.env.local`, database dumps, instance encryption keys, Tap/Electric secrets,
and the persistent SSH host private key are sensitive. Never put them in logs, traces,
issues, or federation records.

## Git and network input

Repository filesystem paths derive from immutable IDs and storage keys, never owner/slug
input. Native Git receives argument arrays without a shell. Revisions, paths, pack streams,
diffs, and metadata are validated, streamed or bounded, cancellable, and configured to
disable repository-controlled helpers where needed. SSH allows only exact upload-pack and
receive-pack commands and rejects shells, PTYs, forwarding, and subsystems.

Cross-instance PR fetch has an explicit SSRF and ref-integrity boundary documented in
[pull request security](pull-requests.md). Tap events are untrusted bounded input and only
authorized record authors control derived status. Sync endpoints expose explicit safe
columns and server-owned predicates.

## Operational posture

Rate limiting, public-alpha hardening, production Compose, automated backup/restore, and
deployment conformance are not complete. Do not expose the current development stack as a
production service. Track [security hardening](https://github.com/tomanagle/adenosine/issues/9)
and the deployment gaps in [self-hosting](self-hosting.md). Report vulnerabilities using
[`../SECURITY.md`](../SECURITY.md), not a public issue.
