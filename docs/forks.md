# Forks And Upstream Synchronization

Adenosine models a fork as an independent public repository with portable ancestry. The
child `dev.adenosine.repo` record contains an optional `forkedFrom` strong reference to its
source repository. The source URI identifies the upstream across instances; its CID records
the exact source record observed at creation time.

## API

```text
POST /api/v1/repositories/{owner}/{repo}/forks
GET  /api/v1/repositories/{owner}/{repo}/forks?limit=20&cursor=...
POST /api/v1/repositories/{owner}/{repo}/sync-fork
```

Creation accepts an optional destination `slug` and `organization`. Forks of a public
source remain public. Collection responses use the standard `{ "items": [], "page": {} }`
envelope; the fork list also returns `fork_count`, which counts direct visible children.

Local sources are copied directly from immutable repository storage paths. Federated
sources use the current canonical HTTPS endpoint from the moderated network projection and
the same DNS and private-address protections as cross-instance pull request fetches. The
initial copy imports only public branches and tags; Adenosine internal refs are excluded.

## Sync safety

`sync-fork` refreshes the upstream repository by its durable AT URI and fetches the fork's
default branch into a temporary internal ref. The visible branch advances only when its
existing head is an ancestor of the upstream head. The final update is compare-and-swap, so
concurrent pushes cannot be overwritten. A divergent fork returns a conflict and retains
its branch unchanged.

Synchronization intentionally does not rewrite other branches, delete refs, or force the
default branch. Pull requests created from a fork target `forkedFrom.uri` by default, while
the source repository and exact source head remain explicit in the proposal record.
