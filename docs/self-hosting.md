# Self-Hosting And Deployment Status

Adenosine is pre-alpha and currently has no supported production deployment. The files in
`dev/` are a development and black-box environment, not a production reference. No backup,
restore, or upgrade automation is shipped yet. The safe current operator action is to
evaluate locally and avoid hosting irreplaceable or sensitive production data.

| Target | Status | Main command today | Cost/scale expectation | Backup/upgrade story and limitations |
|---|---|---|---|---|
| Development Compose | Implemented for local development | `make dev` | One developer machine; multiple containers | Named volumes persist local data, but there is no supported backup/restore or production hardening. Track [#2](https://github.com/tomanagle/adenosine/issues/2). |
| Railway | Not implemented | None | Intended small managed service plus PostgreSQL and persistent Git volume; cost depends on provider resources | No template, Pulumi program, deployment command, SSH validation, backup, or upgrade path. Track [#10](https://github.com/tomanagle/adenosine/issues/10). |
| AWS | Not implemented | None | Intended opinionated single-region deployment with managed PostgreSQL and persistent POSIX Git storage; materially higher baseline cost | No Pulumi stack, recovery automation, or validated scale limits. Track [#12](https://github.com/tomanagle/adenosine/issues/12). |
| Linux VM/systemd | Not implemented | None | Intended single-node self-hosting; operator owns PostgreSQL, TLS, SSH, storage, and monitoring | No units, installer, production config, backup, restore, or rolling-upgrade procedure. |

Production Compose and cloud paths must preserve a POSIX filesystem for bare Git
repositories, a stable SSH host key, PostgreSQL, encryption keys, and public HTTPS/SSH
identity. S3 is not a Git filesystem. Electric is optional and may be rebuilt; application
readiness and REST must not depend on it.

## Backup boundary

A complete future backup must consistently include PostgreSQL (`core.*`, `auth.*`,
`ops.*`, moderation, and projections), every bare Git repository, the SSH host key, OAuth
encryption keys, and a manifest tying versions/configuration together. `network.*` and
Electric state can be replayed, but replay does not replace authoritative local data. A
database-only or Git-only copy is incomplete.

There is currently no command that takes such a consistent backup or proves restore.
Track [backup and restore #11](https://github.com/tomanagle/adenosine/issues/11) and
[deployment conformance #13](https://github.com/tomanagle/adenosine/issues/13).

## Upgrade boundary

Migrations are forward-only and applied at startup, but that alone is not a supported
upgrade process. Until #11 and deployment conformance are complete, there is no validated
preflight, backup, rollback, compatibility matrix, or skipped-version policy. Never infer
that the future commands described in [`../plan.md`](../plan.md) exist.

Observability hardening is tracked in [#1](https://github.com/tomanagle/adenosine/issues/1),
security hardening in [#9](https://github.com/tomanagle/adenosine/issues/9), and a first
release in [#16](https://github.com/tomanagle/adenosine/issues/16).
