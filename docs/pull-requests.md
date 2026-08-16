# Pull Request Fetch And Merge Security

Pull request records are eventually consistent ATProto projections. Before diff or merge,
the target repository host refreshes the exact declared source head into a controlled
`refs/adenosine/pull/<id>/head` ref in its own bare repository.

Remote fetch accepts only canonical HTTPS URLs without credentials, query strings,
fragments, redirects, proxies, cookies, extra headers, credential helpers, or non-HTTPS
protocols. DNS must resolve entirely to public addresses; resolved addresses are pinned
into the Git request to limit DNS rebinding. Branch/ref syntax and full lowercase SHA-1 or
SHA-256 values are validated. Fetches land in a random quarantine ref, the result must be
a commit matching the declared head exactly, and promotion uses compare-and-swap.

Merge requires local repository write permission and an open, unchanged projection. It
refreshes the head again, revalidates the projection, resolves exact commits, computes in
an isolated bare repository with hooks, external diff, signing, and user configuration
disabled, then advances the target branch with compare-and-swap. Supported strategies are
merge commit and squash. Conflicts or changed refs return a conflict rather than replacing
concurrent work.

The Git ref update happens before the durable local merge event and ATProto status
publication. If either later step fails, a retry recognizes the commit trailers and avoids
creating a second merge commit. Operators must still investigate a returned post-ref-update
error using its request ID; REST idempotency is not generally guaranteed.

Neither projected author metadata nor Git commit author strings grant authorization.
Derived PR status is accepted only from the target repository owner, and merge permission
comes from authoritative local repository ownership/collaboration state.

## Requested reviewers

A requested reviewer is a target-repository-authoritative
`dev.adenosine.pullRequestReviewRequest` record. Its deterministic record key binds the
pull request URI and reviewer DID, so retrying the same active request is idempotent. The
record carries exact pull request and target repository CIDs; a request is visible only
while those observations are current. Cancellation deletes the deterministic record with
compare-and-swap protection.

Only an account with current triage permission on the local target repository may request
or cancel a review. New requests are limited to open pull requests and reviewers who can
read the repository. Bilateral blocks, reviewer-hidden pull requests or repositories, and
the requesting viewer's moderation state are enforced before publication or projection.

The REST collection is `GET /api/v1/pull-requests/review-requests` with a required
`pull_request_uri`, bounded `limit`, and opaque keyset `cursor`. It always returns
`{"items":[],"page":{"next_cursor":null}}` shape rather than a top-level array. A maintainer
uses `PUT /api/v1/pull-requests/review-requests/{reviewer}` to request a review and `DELETE`
on the same resource to cancel it. The reviewer path value is a canonical DID; handles are
mutable display and routing identifiers, not durable record identity.

Review-request notifications are local derived inbox rows, not portable canonical records.
An active request produces a stable unread notification for its reviewer. Deletion, a new
pull request CID, repository visibility, blocks, or hides remove it from the derived inbox.
