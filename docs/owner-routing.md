# Owner routing

Adenosine uses one case-insensitive public namespace for account handles and organization
slugs. The same owner name is used by the web routes `/{owner}` and `/{owner}/{repository}`.
Clients must resolve an owner once rather than guessing whether it is an account or an
organization.

## REST resolver

`GET /api/v1/owners/{owner}` returns a small discriminated resource:

```json
{
  "kind": "account",
  "canonical_name": "alice.example",
  "account_did": "did:plc:alice"
}
```

Organization responses use `kind: "organization"` and `organization_slug`. A missing name
returns the standard `404` error envelope. Resolution is case-insensitive, and
`canonical_name` is the spelling clients should use when generating or replacing a URL.
This is a singular resource, so it is not paginated. Collection endpoints continue to use
the standard `{ "items": [], "page": { "next_cursor": null } }` envelope and opaque
keyset cursors.

## Source of truth

`core.owner_routes` owns local alias claims. Account login atomically claims the verified
handle, and organization creation atomically claims its slug. Its case-insensitive unique
index prevents a profile URL and a repository URL from resolving the same text to different
owners. Reserved application paths such as `api`, `explore`, and `login` cannot be claimed.

An account login replaces that account's previous local alias with its current verified
handle. The resolver returns that handle as `canonical_name`, allowing the web route to
normalize case without retaining stale name claims. Local claims always win over the
eventually consistent network projection. Active federated account handles are available
as a read-only fallback. Federated organization slugs are not admitted into the local
authoritative namespace.

The legacy `/profiles/{did}` and `/organizations/{slug}` web routes remain available for
bookmarks and DID-only profiles. New UI links use `/{owner}` whenever a handle or slug is
available.
