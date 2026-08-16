---
status: accepted
date: 2026-08-16
decision-makers: [tomanagle]
consulted: []
informed: []
---

# Model Triage as Owner-Authored Snapshots

## Context and Problem Statement

Issues and pull requests need labels, assignees, and milestones that work across independent
Adenosine instances. The issue author is not necessarily the repository owner, while the
repository owner must remain authoritative for repository workflow. Records can be replayed,
reordered, moderated, or observed before the local AppView catches up. Repository transfers
also change the current author allowed to update workflow metadata without rewriting the
subject's portable identity.

## Decision Drivers

* Remote instances must verify workflow authority from portable records alone.
* A metadata update must not expose a partially applied set of labels or assignees.
* Label and milestone identities must survive renaming and repository transfer.
* Projection must reject forged authority and stale events deterministically.
* Reads must honor local moderation without mutating portable records.
* REST writes must return enough information for optimistic UI while projection converges.

## Considered Options

* Store triage only in local relational join tables
* Publish independent add and remove events for every association
* Publish stable definitions and one owner-authored snapshot per subject

## Decision Outcome

Chosen option: "Publish stable definitions and one owner-authored snapshot per subject",
because it provides an atomic, replay-safe state while keeping every definition independently
addressable.

Repository owners publish `dev.adenosine.repositoryLabel` and
`dev.adenosine.repositoryMilestone` records. Their record keys are stable identities, so
renames and state changes update the same slot. A `dev.adenosine.subjectTriage` record is a
complete snapshot containing the exact subject and repository strong references, label AT
URIs, assignee DIDs, and optional milestone AT URI. Its deterministic record key is derived
from the subject URI, giving each repository authority one current slot per subject.

The projector accepts a record only when its author is the current repository authority,
its repository and subject references agree, every referenced definition belongs to the
same accepted repository lineage, and all collection-specific bounds are valid. Source event
guards make replay and out-of-order delivery idempotent. Definitions authored by a former
owner remain readable through the accepted transfer lineage, but only the current owner can
publish changes. New snapshots are authored by the current owner and may reference effective
definitions retained from that lineage.

The REST API exposes repository-scoped label and milestone resources and an atomic `triage`
subresource for issues and pull requests. Collection reads use bounded, opaque, scope-bound
keyset cursors and object envelopes. A successful portable mutation returns `202 Accepted`,
the pending record, and `projected: false`; the web client updates optimistically and then
refetches the AppView projection.

Projection tables use `TEXT` plus named `CHECK` constraints for finite state values. They do
not use PostgreSQL enum types, which keeps state evolution additive and consistent with the
repository-wide database convention.

### Consequences

* Good, because one record replacement atomically changes all metadata associations.
* Good, because record authorship and exact strong references make authority independently
  verifiable.
* Good, because stable definition keys preserve references across edits and transfers.
* Good, because REST clients can reconcile immediate publication with eventual projection.
* Bad, because concurrent full-snapshot edits are last-write-wins and clients must replace
  the complete set they observed.
* Bad, because effective reads and filters must resolve accepted repository lineage.
* Bad, because deleted definitions can leave historical snapshot URIs that resolve to no
  visible object and must be omitted safely.
* Neutral, because moderation is an AppView concern: two instances may intentionally render
  the same portable snapshot differently for their viewers.

### Confirmation

Compliance is confirmed by Lexicon constraint tests, domain authorization and normalization
tests, AT Protocol CAS tests, federation forgery and source-event guard tests, SQL migration
tests that reject database enum types, REST contract tests for pagination and optimistic
responses, and web build/type checks for the management and filtering flows.

## Pros and Cons of the Options

### Store Triage Only in Local Relational Join Tables

* Good, because relational updates and queries are straightforward on one host.
* Bad, because another instance cannot observe or verify repository workflow state.
* Bad, because local database authority would override portable repository authorship.

### Publish Independent Add and Remove Events for Every Association

* Good, because concurrent changes to different associations can merge.
* Bad, because removals require tombstones or observed-remove set semantics across replay and
  reordering.
* Bad, because users can observe partially delivered metadata and conflict resolution is
  substantially harder to explain.

### Publish Stable Definitions and One Owner-Authored Snapshot per Subject

* Good, because a single CAS-protected replacement is the complete intended state.
* Good, because definitions retain durable identities independent of display text.
* Bad, because every editor must submit the complete metadata set.
* Bad, because lineage-aware projection is required after repository transfer.

## More Information

The public wire contract is documented in [`../api.md`](../api.md), and the ingestion and
eventual-consistency model is documented in [`../federation.md`](../federation.md).
