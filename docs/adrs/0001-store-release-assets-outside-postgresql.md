---
status: proposed
date: 2026-08-16
decision-makers: [Adenosine maintainers]
consulted: []
informed: [Adenosine self-hosters]
---

# Store release assets outside PostgreSQL

## Context and Problem Statement

Repository releases need durable metadata and potentially large downloadable files. Uploads and downloads must remain bounded and streaming-safe, backups must capture the files at the same recovery point as release metadata, and the first implementation must work for a single-node self-hosted installation without preventing a future object-store backend. Where should Adenosine store release metadata and asset bytes, and what deletion contract should it expose?

## Decision Drivers

* Never buffer a complete asset in application or database memory.
* Keep the initial self-hosted topology operationally simple.
* Preserve metadata integrity, immutable tag targets, checksums, and quota accounting transactionally.
* Include assets in the existing portable maintenance-window backup.
* Isolate storage mechanics behind an interface suitable for deterministic fakes and future object storage.
* Make deletion and backup-retention behavior explicit.

## Considered Options

* PostgreSQL metadata plus filesystem blobs behind a storage interface.
* PostgreSQL metadata plus a required S3-compatible object store.
* Store metadata and asset bytes together in PostgreSQL.

## Decision Outcome

Chosen option: "PostgreSQL metadata plus filesystem blobs behind a storage interface", because it satisfies streaming and portability requirements without adding a mandatory service to the supported single-node topology. Release rows snapshot an existing tag's peeled target SHA. Asset metadata records its size, media type, SHA-256 checksum, and opaque storage key; asset bytes are written atomically under `ADENOSINE_RELEASE_ASSET_ROOT`, which defaults to `/var/lib/adenosine/state/release-assets` and is already covered by the instance-state backup.

The application service depends on blob and metadata-store interfaces. Filesystem writes use a temporary file in the target directory, enforce the declared byte count while streaming, sync the file, and rename it atomically. PostgreSQL serializes quota reservation per repository and enforces per-asset, per-release, and per-repository limits. Downloads use the stored checksum as a strong ETag and never derive a filesystem path from an asset name.

Deletion removes live asset bytes immediately before removing their metadata. Deleting a release performs the same operation for each asset before deleting the release row. There is no application-level trash period for release assets; encrypted off-host backups may retain deleted bytes according to the operator's published retention policy.

### Consequences

* Good, because upload and download memory usage is independent of asset size.
* Good, because release metadata remains queryable, constrained, and efficiently paginated in PostgreSQL.
* Good, because the default portable backup already captures the asset directory with instance state.
* Good, because storage keys, checksums, and interfaces allow a future object-store implementation without changing the REST resource model.
* Bad, because database metadata and filesystem bytes cannot be committed in one atomic transaction; interrupted cleanup can require operator repair.
* Bad, because multi-node deployments must share the configured asset root or provide a future object-store implementation.
* Neutral, because backup retention, encryption, authenticity, and legal deletion remain operator responsibilities, consistent with repository storage.

### Confirmation

Compliance is checked by release-service tests, filesystem traversal/symlink/size tests, REST authorization and streaming tests, named database constraints, and the production backup/restore test that preserves the entire instance-state archive.

## Pros and Cons of the Options

### PostgreSQL metadata plus filesystem blobs behind a storage interface

* Good, because it uses the existing persistent volume and backup path.
* Good, because ordinary file streams and atomic rename provide a small implementation surface.
* Good, because the interface keeps callers independent from physical placement.
* Bad, because byte storage and metadata updates have compensating cleanup rather than distributed transactions.
* Bad, because shared storage is required if the application is horizontally replicated.

### PostgreSQL metadata plus a required S3-compatible object store

* Good, because object stores support independent capacity, replication, and multi-node access.
* Good, because content can later be served through a CDN or signed origin.
* Bad, because it makes another stateful service mandatory for every self-hoster.
* Bad, because portable backup and clean restore would need credentials, bucket inventory, and consistency coordination not present in the current topology.

### Store metadata and asset bytes together in PostgreSQL

* Good, because metadata and bytes can commit atomically and share one backup mechanism.
* Good, because no second storage path is required.
* Bad, because large bytea values increase database, WAL, replication, vacuum, and backup pressure.
* Bad, because serving assets through query results complicates bounded streaming and connection-pool behavior.

## More Information

This decision implements [issue #48](https://github.com/tomanagle/adenosine/issues/48). Reconsider the default backend when Adenosine supports horizontal application replicas or incremental portable backups. A future backend must retain opaque keys, exact-size writes, checksum verification, idempotent deletion, and streaming reads.
