---
status: proposed
date: 2026-08-16
decision-makers: [Adenosine maintainers]
consulted: []
informed: [Adenosine operators]
---

# Support selectable S3 release asset storage

## Context and Problem Statement

[ADR 0001](0001-store-release-assets-outside-postgresql.md) keeps release metadata in PostgreSQL and uses filesystem blobs for the supported single-node topology. Horizontally replicated application nodes cannot safely depend on node-local asset files, and a shared POSIX mount couples blob capacity and failure modes to application hosts. How should Adenosine add shared object storage without changing the release REST model or making an object store mandatory for small installations?

## Decision Drivers

* Keep complete assets out of application memory and PostgreSQL.
* Preserve exact-size uploads, SHA-256 verification, opaque keys, immutable reads, and idempotent deletion.
* Allow independent application nodes to share one authoritative blob backend.
* Fail startup when the selected backend, endpoint, bucket, region, or credentials are invalid.
* Keep filesystem storage as the simple default for the supported single-node topology.
* Make metadata/blob consistency, retries, backup, restore, and retention responsibilities explicit.

## Considered Options

* Add a selectable S3-compatible backend while retaining the filesystem default.
* Keep filesystem storage and require a shared POSIX mount for every replicated deployment.
* Make S3-compatible storage mandatory for every installation.

## Decision Outcome

Chosen option: "Add a selectable S3-compatible backend while retaining the filesystem default", because it gives replicated nodes a shared, independently managed blob plane without adding a mandatory service to single-node deployments or changing public release resources.

`ADENOSINE_RELEASE_ASSET_BACKEND` selects `filesystem` or `s3`. S3 mode requires an absolute endpoint, region, bucket, static access-key credentials, and an explicit path-style compatibility flag. Startup uses the current AWS SDK for Go v2 endpoint model and performs `HeadBucket`; invalid or inaccessible configuration prevents the process from serving. All application replicas must use the same PostgreSQL database, bucket, region, endpoint behavior, and credentials policy.

S3 uploads first stream into a process-local temporary file while enforcing the declared size and computing SHA-256. The verified, seekable file is uploaded with `Content-Length`, an S3 SHA-256 checksum, checksum metadata, and `If-None-Match: *`. This makes SDK retries repeatable and prevents overwriting a different object. A conditional conflict is accepted only when the existing object's size and checksum match, which reconciles an ambiguous successful write after a lost response. Downloads stream from `GetObject` and verify checksum metadata at EOF. Object keys remain UUID-derived and never contain the user-facing file name.

Deletion removes the object before PostgreSQL metadata, as in the filesystem implementation. `DeleteObject` is idempotent, so a metadata failure can safely retry the same deletion. A metadata-insert failure triggers compensating object deletion; if both operations fail, the object is an orphan and must be reconciled after a grace period. Bucket versioning or provider retention may preserve non-current bytes after the live delete and therefore defines legal and disaster-recovery retention.

The bundled portable backup and restore commands remain filesystem-only and fail closed in S3 mode. S3 operators must coordinate a write barrier, PostgreSQL recovery point, bucket version/inventory recovery point, credentials, and lifecycle policy through provider tooling. Restoring only one side can create missing objects or unreferenced objects and is not a valid Adenosine recovery.

### Consequences

* Good, because multiple stateless application nodes can share release assets without a shared application filesystem.
* Good, because the release REST contract, metadata schema, quotas, checksums, and cache behavior do not depend on the physical backend.
* Good, because conditional writes and same-checksum reconciliation make uncertain network retries safe.
* Bad, because S3 uploads require temporary local disk at least as large as the maximum single asset.
* Bad, because PostgreSQL and S3 still cannot commit atomically; compensating cleanup and operator reconciliation remain necessary.
* Bad, because S3 backup, version retention, encryption, lifecycle policy, and recovery testing become provider-owned operations.
* Neutral, because filesystem remains the default and continues to use the bundled maintenance-window backup.

### Confirmation

Compliance is checked by one deterministic blob-store contract shared by filesystem and S3 implementations, an S3 protocol fixture that exercises signing and SDK request/response behavior, concurrent cross-instance write tests, checksum-corruption and transient-retry tests, configuration validation, startup bucket validation, operation-script fail-closed tests, and the unchanged release REST suite.

## Pros and Cons of the Options

### Add a selectable S3-compatible backend while retaining the filesystem default

* Good, because operators choose complexity appropriate to their topology.
* Good, because S3-compatible services and AWS S3 use the same application boundary.
* Good, because object capacity, replication, lifecycle, and durability can be managed independently.
* Bad, because two backend implementations and two recovery procedures must remain tested.
* Bad, because static application credentials require careful rotation and least-privilege policy.

### Keep filesystem storage and require a shared POSIX mount

* Good, because the implementation and portable backup remain unchanged.
* Good, because temporary staging and final storage use ordinary filesystem primitives.
* Bad, because every replica depends on the same mounted filesystem semantics and availability.
* Bad, because blob scaling and application-host storage remain coupled.
* Bad, because a nominally shared filesystem may not preserve the atomicity and locking assumptions of the local implementation.

### Make S3-compatible storage mandatory for every installation

* Good, because all deployments use one blob topology and recovery model.
* Good, because horizontal scaling does not require a later backend migration.
* Bad, because single-node self-hosters must operate or purchase another stateful service.
* Bad, because the bundled portable backup can no longer capture a complete installation by itself.
* Bad, because development and isolated installations become dependent on network object storage.

## More Information

This decision extends rather than supersedes [ADR 0001](0001-store-release-assets-outside-postgresql.md). Reconsider static credentials when the maintained deployment targets can supply provider-native workload identity consistently, and reconsider the filesystem default if the supported production topology becomes horizontally replicated by default.
