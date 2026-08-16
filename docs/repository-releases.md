# Repository Releases

Releases are local, authoritative resources attached to an existing Git tag. Creating a release
snapshots the tag's peeled object ID in `target_sha`; moving or deleting the Git tag later does not
retarget the release. Release notes are Markdown, and the web application renders them through the
same HTML-disabled safe renderer used for repository documents and collaboration bodies.

## REST resources

The complete contract and generated-client shapes are in [`../api/openapi.yaml`](../api/openapi.yaml).
The resource hierarchy is:

```text
/api/v1/repositories/{owner}/{repo}/releases
/api/v1/repositories/{owner}/{repo}/releases/{release}
/api/v1/repositories/{owner}/{repo}/releases/{release}/assets
/api/v1/repositories/{owner}/{repo}/releases/{release}/assets/{asset}
```

Collection responses are objects with `items` and `page`; release lists additionally expose
`viewer_can_manage`. `limit` is bounded to 1–100, defaults to 30, and `cursor` is an opaque,
collection-bound keyset cursor. Clients must not decode or construct cursors.

Published releases and their assets follow normal repository read visibility. Drafts return 404 to
anyone without repository write permission so their existence is not disclosed. Repository writers
may create, update, publish, unpublish, and delete releases and assets. Browser-session mutations
also require the configured exact `Origin`; personal access tokens require repository write scope.
Archived repositories reject mutations.

## Streaming assets

Upload an asset with a raw request body:

```http
POST /api/v1/repositories/{owner}/{repo}/releases/{release}/assets?name=project.tar.gz
Content-Type: application/octet-stream
Content-Length: 12345
X-Asset-Content-Type: application/gzip
```

`Content-Length` and `X-Asset-Content-Type` are required. The server rejects unsafe file names,
streams at most the declared size plus one byte, verifies the exact byte count, computes SHA-256,
and publishes the bytes immutably through the selected storage backend. It never uses the supplied
file name as a storage path. Metadata creation reserves quota under a repository-scoped PostgreSQL
lock; a failed reservation removes the staged blob.

Downloads return the recorded media type and length, `Content-Disposition: attachment`,
`X-Content-Type-Options: nosniff`, `X-Checksum-SHA256`, and a strong `"sha256:…"` ETag. Public
assets use `Cache-Control: public, max-age=31536000, immutable`; private assets use the same lifetime
with `private`. `If-None-Match` supports a 304 response. Asset URLs contain immutable UUIDs, and
replacing a file requires deleting it and uploading a new asset.

## Storage backends and capacity

The default backend is the instance filesystem:

```dotenv
ADENOSINE_RELEASE_ASSET_BACKEND=filesystem
ADENOSINE_RELEASE_ASSET_ROOT=/var/lib/adenosine/state/release-assets
```

Replicated application nodes must use one shared S3-compatible bucket instead of node-local files.
Configure every node identically:

```dotenv
ADENOSINE_RELEASE_ASSET_BACKEND=s3
ADENOSINE_RELEASE_ASSET_S3_ENDPOINT=https://objects.example.com
ADENOSINE_RELEASE_ASSET_S3_REGION=us-east-1
ADENOSINE_RELEASE_ASSET_S3_BUCKET=adenosine-release-assets
ADENOSINE_RELEASE_ASSET_S3_ACCESS_KEY_ID=REDACTED
ADENOSINE_RELEASE_ASSET_S3_SECRET_ACCESS_KEY=REDACTED
ADENOSINE_RELEASE_ASSET_S3_SESSION_TOKEN=
ADENOSINE_RELEASE_ASSET_S3_PATH_STYLE=false
```

Set path style to `true` only for providers that require bucket names in request paths. The endpoint
may use HTTP for isolated local testing; production credentials must use HTTPS. Startup validates
the complete selection and calls `HeadBucket`, so an invalid endpoint, missing bucket, or rejected
credential prevents the node from serving. Credentials need the bucket-level permission required
by `HeadBucket` (normally `s3:ListBucket`) plus object get, put, head, and delete permissions. Do not
grant public object access; downloads remain authorized and streamed by Adenosine.

S3 uploads use local temporary disk to verify the declared size and SHA-256 before `PutObject`.
Provision at least the per-asset limit as temporary capacity on every uploading node. Conditional
creation prevents a node from replacing a different object, while matching size/checksum
reconciliation makes a lost successful response safe to retry. The SDK uses its bounded standard
retry policy for transient failures. Downloads verify checksum metadata while streaming; a mismatch
terminates the response as a storage-integrity failure.

Capacity defaults are 100 MiB per asset, 1 GiB per release, and 10 GiB across a repository.
Operators may set byte counts with:

```dotenv
ADENOSINE_RELEASE_ASSET_MAX_BYTES=104857600
ADENOSINE_RELEASE_MAX_BYTES=1073741824
ADENOSINE_REPOSITORY_RELEASE_MAX_BYTES=10737418240
```

The limits must be positive and monotonically increasing.

## Backup, restore, retention, and deletion

For filesystem storage, the production portable backup captures the default asset root in the
instance-state archive. An operator who moves the root outside instance state must extend their
backup and clean-restore procedure to capture it at the same maintenance barrier; the bundled
scripts do not discover arbitrary external paths.

The bundled backup and restore scripts deliberately fail closed when S3 is selected because their
archive does not contain the external bucket. S3 operators need a provider-specific recovery
procedure that:

1. Stops every write-capable Adenosine node at one maintenance barrier.
2. Records a PostgreSQL backup and a bucket version, snapshot, or immutable inventory from that
   barrier.
3. Preserves endpoint/region/path-style configuration, credentials or workload identity, bucket
   encryption keys, lifecycle policy, and the application image/schema version.
4. Restores PostgreSQL and the bucket to the same recovery point before starting one node and
   validating asset downloads, then adds the remaining nodes.

Restoring only PostgreSQL can leave metadata pointing at missing objects; restoring only the bucket
can leave unreferenced objects. Treat either condition as an incomplete disaster recovery, not as
eventual consistency. Test the paired restore and credential rotation regularly.

Deletion first marks a release as internally deleting, which hides it and prevents concurrent
uploads. Blob deletion and metadata cleanup are idempotent, so retrying a failed deletion resumes
the operation. If metadata creation fails after an upload, Adenosine immediately attempts a
compensating blob delete. A second storage failure can leave an orphan; after a maintenance barrier
and a grace period, operators may compare bucket keys with `core.release_assets.storage_key` and
remove unreferenced objects. Never run that reconciliation concurrently with uploads.

Live filesystem files have no trash retention period. An unversioned S3 delete removes the live
object; versioned buckets may retain prior versions or delete markers until their lifecycle policy
expires them. Copies in encrypted backups remain until the operator's declared backup-retention
policy expires them.

The storage rationale is recorded in
[ADR 0001](adrs/0001-store-release-assets-outside-postgresql.md). The selectable S3 backend and its
consistency model are recorded in
[ADR 0002](adrs/0002-support-selectable-s3-release-asset-storage.md).
