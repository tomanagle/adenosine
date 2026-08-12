# Railway deployment

The maintained Railway target is `infra/pulumi/railway`. It provisions a project,
PostgreSQL, one Adenosine replica with a persistent volume mounted at
`/var/lib/adenosine`, separate web and Caddy gateway services, optional Electric and Tap
services, an OTLP Collector, and a custom HTTPS domain. The application image is rejected
unless it is pinned by OCI digest. Service variables are authoritative and stale managed
variables are removed.

## Bootstrap and update

Install Node dependencies in `infra/pulumi/railway`, log in to Pulumi and Railway, then set:

```sh
export RAILWAY_API_TOKEN='...'
export ADENOSINE_DOMAIN='forge.example.com'
export ADENOSINE_IMAGE='ghcr.io/example/adenosine@sha256:...'
export ADENOSINE_OAUTH_STATE_KEY='base64-encoded-32-byte-key'
export ADENOSINE_OAUTH_CREDENTIAL_KEY='base64-encoded-32-byte-key'
scripts/deploy-railway.sh --stack production --yes --skip-conformance
```

Omit `--skip-conformance` only after setting the variables in
`test/deployment/conformance.env.example`. The script is rerunnable: it selects the same
stack, updates config, applies Pulumi, waits for the deployment whose pre-deploy migration
succeeded, and runs the common suite. It does not print tokens, database passwords, or key material.

Railway DNS verification records are visible in the Railway dashboard/API after the custom
domain is created. Configure them before expecting readiness. A workspace can be selected
with `pulumi config set workspaceId ...`; region and feature toggles are normal Pulumi
config (`region`, `enableElectric`, and `enableTap`).

## Storage, backup, and rollback

Railway mounts volumes as root. The app service sets `RAILWAY_RUN_UID=0` only for its startup
entrypoint, initializes ownership, then executes both SSH and HTTP application listeners as
the unprivileged `adenosine` account. Volume creation precedes deployment. Database migration
runs through Railway's pre-deploy command before a replacement can serve; pre-deploy never
touches the volume.

The single application volume contains both native bare repositories and the persistent SSH
host key. PostgreSQL uses a separate volume. Enable Railway volume backup schedules and
retain at least `backupRetentionDays`; that input records policy intent because Railway's
public API currently exposes schedule listing but not schedule creation. Provider snapshots
are not a portable backup. A complete portable backup must capture PostgreSQL, the whole
application volume, OAuth keys, Tap state when enabled, and an image/config manifest.

Rollback means restoring that complete backup and selecting the prior image digest. Database
migrations are forward-only, so changing only the image is not a database rollback. Test
restore with the conformance hooks before relying on it.

## Limitations and cost

Railway custom domains route HTTP. This program does not create an undocumented TCP proxy,
so `sshHost` is explicitly null. Official Railway conformance therefore fails until an
operator creates a supported TCP proxy for port 2222 and configures SSH, telemetry, and
portable backup/restore hooks; it never reports a partial deployment as conformant. Git HTTPS
remains fully standard.
Railway volumes bind the service to one region and one replica. Costs include five services
with all features enabled, persistent volumes, egress, and backups; disable Electric or Tap
when those documented optional capabilities are not needed.
