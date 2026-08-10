# Security Policy

Adenosine is pre-alpha software and does not yet have supported releases.

Please report suspected vulnerabilities privately to the project maintainers rather than opening a public issue. Include the affected revision, reproduction steps, impact, and any suggested mitigation. Do not include live credentials or private repository data.

Security-sensitive areas include Git command execution, path resolution, authentication, authorization, SSRF, OAuth sessions, SSH handling, and resource exhaustion.

The current development Compose stack is not supported for production and automated
backup/restore, deployment hardening, and public-alpha security work remain incomplete.
See the [security model](docs/security.md) and
[self-hosting status](docs/self-hosting.md) before evaluating an instance.
