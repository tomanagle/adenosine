# Self-Hosting With Production Compose

`deploy/docker-compose.yml` is the supported single-node production topology. It runs Caddy,
the Adenosine API/SSH server, the server-rendered web application, PostgreSQL, and an OpenTelemetry
Collector. Electric and Tap are optional Compose profiles. PostgreSQL, bare Git repositories,
instance state (including the SSH host key), Caddy state, Electric state, and Tap state use named
volumes. Bare Git storage requires a local POSIX filesystem; do not mount object storage as the
repository volume.

The immutable Adenosine image contains both the Go application and built TanStack Start assets.
Compose runs the same image as separate `app` and `web` roles; the web role receives only an
ephemeral writable home/tmpfs for Bun runtime caches. Provider stacks that expose only the API must
add this image's `web` role or another static/SSR web deployment before claiming UI conformance.

## Host requirements

- A Linux host with current Docker Engine and the Compose plugin
- A public DNS name with TCP 80 and 443 routed to the host
- A public SSH port routed directly to the configured Compose SSH port (22 by default)
- `curl`, `openssl`, and enough local/off-host storage for full backups

The release tag is part of the deployment configuration. Production never consumes `latest`.

## First installation

From a release checkout, initialize configuration and durable SSH identity:

```sh
scripts/bootstrap.sh --domain code.example.com --version v0.1.0
```

Bootstrap creates `deploy/.env` with mode `0600`, generates each missing secret exactly once,
creates the repository and instance-state volumes, and creates the Ed25519 SSH host key if absent.
Rerunning it preserves every existing value and key. It does not print secrets. Review ports and
the immutable image/tag in `deploy/.env`, then apply migrations and start services:

```sh
scripts/migrate.sh
docker compose --env-file deploy/.env -f deploy/docker-compose.yml up -d
scripts/doctor.sh
```

Migrations are an explicit operator action. Application replicas never migrate on startup.
Caddy obtains and renews public certificates. Smart HTTP Git requests bypass response buffering
and compression and have long body/header timeouts. SSH does not pass through Caddy; if port 22 is
already occupied, set `ADENOSINE_SSH_PORT` to another public port before bootstrap completes.

## Optional federation and realtime services

The default stack remains correct without Electric. To enable it, set:

```dotenv
ADENOSINE_ELECTRIC_URL=http://electric:3000
ADENOSINE_ELECTRIC_SECRET=<same value as ELECTRIC_SECRET>
COMPOSE_PROFILES=electric
```

Run `scripts/migrate.sh` again to idempotently configure the least-privilege replication role and
publication, then start the stack. To enable Tap, set a unique consumer such as
`ADENOSINE_TAP_CONSUMER=tap:code.example.com:v1` and include `tap` in the comma-separated
`COMPOSE_PROFILES`. `scripts/doctor.sh` reports enabled Electric/Tap failures as hard failures;
Collector reachability and external ATProto reachability are warnings so they cannot take Git or
REST offline.

The included Collector validates and emits OTLP telemetry to its logs. Replace the exporter in
`deploy/otel-collector.yml` with a durable production telemetry backend when required.

## Diagnostics

```sh
scripts/doctor.sh
```

Doctor checks PostgreSQL and schema state, HTTPS readiness and OpenAPI, native Git, repository
writability, SSH host key permissions, public URL consistency, disk pressure, enabled Tap and
Electric/logical replication, outbound ATProto connectivity, and Collector reachability. It never
prints configuration secrets.

## Backup

```sh
scripts/backup.sh --output /secure/adenosine-backups
```

Backup uses a maintenance window: Caddy and all write-capable application/federation services stop,
then `pg_dump` captures PostgreSQL and tar captures repositories and instance state. This barrier
ensures committed repository rows refer to captured repositories without claiming a distributed
transaction. Services restart even when capture fails. The package contains:

- `manifest.json` with format, release, schema, timestamp, consistency mode, and RPO
- a PostgreSQL custom-format dump
- repository and instance-state archives, including the stable SSH identity
- release assets from `/var/lib/adenosine/state/release-assets` (or the configured `ADENOSINE_RELEASE_ASSET_ROOT` when it remains beneath instance state)
- the identity/decryption-critical production environment
- SHA-256 checksums for every payload

The resulting archive contains secrets and local Git objects that cannot be rebuilt from ATProto.
It also contains hosted release assets. Live deletion is immediate, while copies in backups remain
until the operator's encrypted off-host retention policy expires them.
Encrypt it at rest, restrict access, copy it off-host, and apply an organization-specific retention
policy. The script supplies integrity checksums, not authenticity signatures; sign the archive or
its checksum with the operator's existing signing system. RPO is all state committed before the
maintenance barrier. RTO is dominated by image pulls, dump restore, and repository archive size;
measure it with periodic restore drills. Incremental backup and PITR are not included.

The maintenance marker is `deploy/.maintenance`. Backup, migration, upgrade, restore, and doctor
share this state, so nested operations cannot accidentally reopen traffic. If an interrupted
operation leaves the marker behind, inspect the stopped services and data before removing it and
starting the stack; do not remove it merely to silence doctor output.

## Clean restore

Restore only into a host/project whose three authoritative volumes are empty:

```sh
scripts/restore.sh --backup /secure/adenosine-backups/adenosine-v0.1.0-TIMESTAMP.tar.gz --confirm-clean
```

Restore validates the adjacent `<archive>.sha256` automatically when present; pass another outer
digest with `--checksum`. It rejects unknown formats, malformed manifests, release/schema
incompatibility, missing payloads, checksum failures, unsafe archive paths, and
non-empty PostgreSQL/repository/instance-state volumes before replacing data. It installs the
backed-up environment, restores PostgreSQL and files, migrates forward if needed, starts services,
and runs doctor. Failed clean restores remove the newly created project volumes and restore the
selected env file, making the same command safely retryable. It never regenerates secrets or the SSH host key. A backup requiring an unavailable
image or unsupported future format fails closed. After restore, rebuild/reindex optional Electric
and network projections and verify ordinary Git clone/push and the recorded SSH fingerprint.

## Upgrade and rollback

Read `docs/releases/<version>.md`, then run:

```sh
scripts/upgrade.sh --version v0.1.1 --backup-dir /secure/adenosine-backups
```

Upgrade verifies published release notes, runs doctor, takes a maintenance backup, switches to the
immutable image, explicitly migrates, restarts, and runs doctor again. On failure it prints an exact
rollback command. Database migrations are forward-only. Roll back the application only after the
target release notes confirm schema compatibility:

```sh
scripts/rollback.sh --version v0.1.0 --confirm-schema-compatible
```

Rollback never reverses schema. If releases are incompatible, perform a clean restore of the
pre-upgrade backup instead.

## Scope and limitations

This topology is a single-node deployment with a maintenance-window backup, not high availability.
Caddy state and rebuildable Electric/Tap caches are persistent operational state but are not in the
portable authoritative backup. Operators own host patching, firewalling, encrypted off-host backup,
monitoring retention, capacity planning, and restore drills. Railway and AWS provider stacks remain
separate work; all must preserve the same PostgreSQL, POSIX Git, instance-secret, and SSH-identity
contracts.
