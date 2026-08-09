# Repository Read API

Git remains authoritative for repository refs and objects. Adenosine exposes the minimum read API needed for a third-party repository browser:

```text
GET /api/v1/repositories/{owner}/{repo}/branches
GET /api/v1/repositories/{owner}/{repo}/tags
GET /api/v1/repositories/{owner}/{repo}/tree?rev=main&path=docs
GET /api/v1/repositories/{owner}/{repo}/blobs/{sha}
GET /api/v1/repositories/{owner}/{repo}/commits?ref=main&limit=30
GET /api/v1/repositories/{owner}/{repo}/commits/{revision}
GET /api/v1/repositories/{owner}/{repo}/diff?base={revision}&head={revision}
GET /api/v1/repositories/{owner}/{repo}/merge-base?a={revision}&b={revision}
```

Public repositories are anonymously readable. Private repositories require an authorized session or a PAT with `repository:read` or `repository:write`; repository-restricted PATs must match the requested repository. Unauthorized private resources are concealed as not found after authentication.

Branch responses expose only `refs/heads`, and tag responses expose only `refs/tags`. Annotated tags include both the tag object and peeled target. Tree responses resolve the requested revision to a commit and return one immediate directory level, ordered with trees first and then bytewise by name. Tree, blob, and submodule entries are distinguished by object type and mode.

Revisions, paths, and object IDs are untrusted input. Adenosine rejects option-like or control-containing revisions, absolute and traversal-like paths, invalid UTF-8 paths, and abbreviated blob IDs. Native Git commands use argument arrays without a shell, machine-safe output formats, immutable repository IDs, bounded metadata output, and cancellable process groups.

Blob responses contain raw bytes rather than JSON or Base64. Blob type and size are verified before headers are written, then `git cat-file` streams directly to the HTTP response. Immutable object IDs provide the `ETag`; public and private repositories receive corresponding immutable cache policies.

Commit history is newest first and bounded to 1-100 entries. Commit responses preserve parents, author and committer identities, timestamps, summary, and the full UTF-8 message. Git author metadata is repository content and is not treated as an authenticated Adenosine identity.

Diff responses resolve both revisions to full commit IDs and include path status, rename paths, nullable line counts for binary files, and a bounded UTF-8 patch. External diff commands and text conversion are disabled so repository configuration cannot execute helpers. Oversized diffs fail with `413 git_output_too_large` rather than being silently truncated. Merge-base responses expose the resolved common commit or return not found when histories are unrelated.
