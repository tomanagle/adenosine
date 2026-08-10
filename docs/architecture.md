# Architecture

## Sources of truth

Adenosine deliberately has several authorities rather than one database model:

| Concern | Authority | Local representation |
|---|---|---|
| repository objects, commits, trees, and refs | bare Git repository | filesystem selected by immutable `core.repositories.storage_key` |
| person/account identity | AT Protocol DID | `core.accounts.did`; handles are mutable caches |
| portable repository identity and collaboration records | canonical AT URI and CID | local repository publication metadata or `network.*` projections |
| local accounts, repositories, aliases, collaborators | `core.*` | authoritative PostgreSQL state for this instance |
| sessions, OAuth credentials, PATs, SSH keys, passkeys | `auth.*` | sensitive local state; never federated |
| profiles, repository discovery, stars, issues, comments, pull requests | ATProto repositories and event stream | rebuildable, eventually consistent `network.*` projections |
| cursors, receipts, and durable internal work | `ops.*` | operational state, not a public social record |

A DID identifies a person even when its handle changes. An AT URI such as
`at://did:plc:.../dev.adenosine.repo/<rkey>` identifies the portable repository record;
it is not the Git storage path. A CID identifies an exact record revision. Strong
references use both URI and CID so stale collaboration records cannot silently retarget.

Git is never reconstructed from `network.*`. PostgreSQL discovery rows may be replayed,
but the bare repository and its refs must be backed up. Conversely, Git author and
committer strings are repository content, not authenticated DID claims.

## Boundaries

The dependency direction is transport adapters to application/domain services to narrow,
consumer-owned capability interfaces to infrastructure adapters. `cmd/adenosine/main.go`
is intentionally only startup composition. Packages that must produce a startup value own
a public `Must` function and a private error-returning implementation. Those startup-only
functions may panic because the process cannot run without the value. Request paths and
background runtime work return errors wrapped with operation context and preserve
`errors.Is`/`errors.As`; they do not panic.

The Go service owns REST, Git Smart HTTP, SSH, authorization, ATProto publication, Tap
projection, and the Electric proxy. The first-party web application has no private data
path: it uses the same generated REST client and documented sync endpoints as other
clients. REST is authoritative for writes. Electric is an optional projection read path.

## Consistency and failures

Local Git reads, clones, fetches, pushes, and authorized local ref changes operate against
local Git and local authorization state. They do not perform request-time calls to another
Adenosine instance. An ATProto/PDS/Relay/Tap outage can delay identity login, publication,
or network projections, but it must not make already-authorized local Git data depend on
the network.

Federated writes may return an AT URI/CID before Tap has projected the new revision into
`network.*` or Electric has delivered it. Clients must represent that interval as pending
or delayed, not as data loss. Projectors use event IDs, receipts, and record-specific stale
guards so duplicate or older delivery is a no-op. See [federation](federation.md) and
[realtime](realtime.md).

The historical rationale and roadmap remain in [`../plan.md`](../plan.md); current code,
OpenAPI, migrations, Lexicons, and this documentation take precedence for implemented
behavior.
