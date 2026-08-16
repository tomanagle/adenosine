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
syncs a temporary file, and atomically renames it. It never uses the supplied file name as a
storage path. Metadata creation reserves quota under a repository-scoped PostgreSQL lock; a failed
reservation removes the staged blob.

Downloads return the recorded media type and length, `Content-Disposition: attachment`,
`X-Content-Type-Options: nosniff`, `X-Checksum-SHA256`, and a strong `"sha256:…"` ETag. Public
assets use `Cache-Control: public, max-age=31536000, immutable`; private assets use the same lifetime
with `private`. `If-None-Match` supports a 304 response. Asset URLs contain immutable UUIDs, and
replacing a file requires deleting it and uploading a new asset.

## Capacity, backup, and deletion

Defaults are 100 MiB per asset, 1 GiB per release, and 10 GiB across a repository. Operators may
set byte counts with:

```dotenv
ADENOSINE_RELEASE_ASSET_MAX_BYTES=104857600
ADENOSINE_RELEASE_MAX_BYTES=1073741824
ADENOSINE_REPOSITORY_RELEASE_MAX_BYTES=10737418240
```

The limits must be positive and monotonically increasing. `ADENOSINE_RELEASE_ASSET_ROOT` defaults
to `/var/lib/adenosine/state/release-assets`. The production portable backup captures that path in
the instance-state archive. An operator who moves the root outside instance state must extend their
backup and clean-restore procedure to capture it at the same maintenance barrier; the bundled
scripts do not discover arbitrary external paths.

Deletion first marks a release as internally deleting, which hides it and prevents concurrent
uploads. Blob deletion and metadata cleanup are idempotent, so retrying a failed deletion resumes
the operation. Live files have no trash retention period. Copies in encrypted backups remain until
the operator's declared backup-retention policy expires them.

The storage rationale and future object-store boundary are recorded in
[ADR 0001](adrs/0001-store-release-assets-outside-postgresql.md).
