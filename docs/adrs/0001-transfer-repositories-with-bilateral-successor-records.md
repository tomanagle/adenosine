---
status: accepted
date: 2026-08-14
decision-makers: [tomanagle]
consulted: []
informed: []
---

# Transfer Repositories With Bilateral Successor Records

## Context and Problem Statement

A public repository's AT URI contains the DID that authored its repository record. Changing
only `core.repositories.owner_did` would therefore make local authorization disagree with
portable identity, while moving the record to another AT repository necessarily creates a
different URI. Repository transfer must require both owners, preserve old routes and Git
clients, keep existing collaboration records meaningful, and converge when records arrive
out of order on independent Adenosine instances.

## Decision Drivers

* Neither the source nor destination owner can transfer a repository unilaterally.
* AT Protocol record authorship remains the authority boundary; a database update cannot
  impersonate a different DID.
* Existing Git URLs, repository URIs, forks, issues, pull requests, and webhook attachment
  must not silently break.
* Federation projection is eventually consistent and must tolerate replay, reordering,
  partial delivery, conflicts, and temporary credential failure.
* Transfer initiation must be cancelable, while completed ownership history must be
  immutable and auditable.
* Local storage continues to use the repository UUID, so ownership changes never move Git
  data on disk.

## Considered Options

* Change only local owner metadata
* Keep the original repository URI forever and delegate all future authority
* Create a bilaterally authorized successor record and preserve a lineage
* Copy the repository without a portable transfer relationship

## Decision Outcome

Chosen option: "Create a bilaterally authorized successor record and preserve a lineage",
because it keeps AT Protocol authorship truthful, gives both owners a signed part in the
transition, and lets every previous identity remain resolvable without treating a mutable
route or local database row as network authority.

A transfer uses two portable records:

1. The current owner publishes a `dev.adenosine.repositoryTransfer` proposal containing
   the exact source repository strong reference, destination owner, destination route, and
   expiry.
2. The destination owner publishes the successor `dev.adenosine.repo` record and a
   `dev.adenosine.repositoryTransferAcceptance` record containing exact strong references
   to both the proposal and successor.

The source repository record is then updated with `transferredTo`, and the successor record
contains `transferredFrom`. A projector recognizes a completed edge only when proposal
authorship matches the source authority, acceptance authorship matches the declared
destination, all strong references match exact CIDs, and the repository records point back
to each other. Unilateral, conflicting, expired, cyclic, or incomplete edges remain visible
as transfer records but do not change canonical ownership.

The first repository URI is the immutable lineage identity; the newest fully accepted
successor is the canonical current record. APIs return both where relevant. New
collaboration records target the canonical record. Existing records keep their original
strong references and are grouped through the lineage rather than rewritten.

Local transfer states are `pending`, `completed`, and `cancelled`. Initiation requires
current repository admin authorization and creates at most one pending transfer for a
repository. Acceptance requires the destination account, or an owner of the destination
organization, and rejects route conflicts before publication. Cancellation requires source
admin authorization and is allowed only while pending.

Acceptance is an idempotent, retryable workflow with deterministic record keys. The
destination first records an `acceptance_started_at` timestamp inside the proposal window,
then publishes the successor, acceptance, and source redirect records before atomically
changing local ownership. Every portable write uses that durable timestamp, so recovery is
deterministic and may continue after the proposal expires. A failure leaves the transfer
pending with its published identities recorded so a retry continues rather than duplicating
records. Once acceptance starts, cancellation is forbidden because a portable write may
already have succeeded even if its local identity was not persisted; recovery completes the
workflow. Reversing a completed transfer requires a new transfer in the opposite direction.

Every prior owner/slug route remains an alias to the same local UUID. Old Smart HTTP and SSH
clone URLs therefore continue to work directly. Web pages may advertise the canonical
route, but Git transport does not depend on redirects. Fork ancestry and collaboration
lookups resolve any URI in the accepted lineage. Repository webhooks remain attached to the
stable local repository UUID; only future authorization uses the new owner.

### Consequences

* Good, because both parties produce independently verifiable authorization records.
* Good, because old clone URLs and collaboration references continue resolving without
  rewriting Git storage or federated records.
* Good, because deterministic keys and exact strong references make replay and recovery
  idempotent.
* Good, because independent instances can derive the same canonical successor from public
  records without trusting another forge's local transfer table.
* Bad, because repository identity becomes a lineage and all repository resolvers must be
  transfer-aware.
* Bad, because acceptance spans multiple PDS writes and a local transaction, so partial
  publication requires explicit retry and operational visibility.
* Bad, because completed transfers cannot be rolled back by deleting history; ownership
  must be transferred back with another bilateral operation.
* Neutral, because existing issues, pull requests, forks, and webhooks retain their stored
  identifiers and gain lineage-aware resolution rather than bulk mutation.

### Confirmation

Compliance is confirmed by domain tests for both-party authorization and retry states,
database tests for route aliases and keyset history, REST contract tests for every transfer
resource, Git transport tests for old and new routes, and a two-instance federation test
that delivers proposal, acceptance, and repository records in different orders and reaches
the same canonical owner.

## Pros and Cons of the Options

### Change Only Local Owner Metadata

* Good, because it is a small local transaction.
* Bad, because the AT URI continues naming the old DID and remote instances cannot observe
  or verify the ownership change.
* Bad, because it makes local database state override portable authorship.

### Keep the Original Repository URI Forever and Delegate All Future Authority

* Good, because every collaboration reference remains unchanged.
* Good, because identity continuity is simple for consumers.
* Bad, because all future repository metadata needs a second authority-record overlay and
  clients that understand only `dev.adenosine.repo` see stale ownership and endpoints.
* Bad, because delegation chains become the permanent write authority for a record still
  located in the former owner's AT repository.

### Create a Bilaterally Authorized Successor Record and Preserve a Lineage

* Good, because the current record lives under and is authored by its current owner.
* Good, because the source proposal and destination acceptance prove mutual consent.
* Good, because old identities remain durable aliases in a verifiable chain.
* Bad, because canonical resolution and counters must operate over a lineage.
* Bad, because acceptance needs recoverable multi-record publication.

### Copy the Repository Without a Portable Transfer Relationship

* Good, because it uses existing repository creation behavior.
* Neutral, because users can manually redirect old routes on one forge.
* Bad, because forks and collaboration history split across unrelated identities.
* Bad, because remote instances cannot distinguish a transfer from an unrelated copy.

## More Information

This decision resolves the repository-transfer question recorded as ADR-002 in `plan.md`.
It builds on the existing invariant that repository storage paths use immutable local UUIDs
and that owner/slug values are aliases rather than storage identity.
