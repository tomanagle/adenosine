# Git Smart HTTP

Adenosine delegates Git object and pack protocol behavior to the configured native Git executable. Pack request and response bodies stream directly between HTTP and `git upload-pack` or `git receive-pack`; they are never accumulated in application memory.

Public repositories support anonymous clone and fetch through:

```text
GET  /<owner>/<repository>.git/info/refs?service=git-upload-pack
POST /<owner>/<repository>.git/git-upload-pack
```

Push uses the corresponding `git-receive-pack` discovery and RPC endpoints. Supply a personal access token as the HTTP Basic password. The token must be active, include the `repository:write` scope, satisfy any repository restriction on the token, and belong to the repository owner or a collaborator with write access.

After a successful receive-pack request, Adenosine records a `git.push_received` event in the PostgreSQL outbox for asynchronous post-push work. Branch updates, tag updates, and deletions use the same path.

Repository paths are resolved from PostgreSQL metadata and immutable repository IDs. Public owner and slug values are never interpolated into filesystem paths.

The transport supports Git protocol versions 1 and 2 through the standard `Git-Protocol` header.
