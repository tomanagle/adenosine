# Lexicons, Publication, And Tap

[`../lexicons/`](../lexicons/) is the implementation-independent source of truth for
`dev.adenosine.*` records: profile, repository, star, issue/status/comment, pull
request/status/review, and repository triage. A DID is the record author; record fields do not duplicate mutable
handles or claim a different author. Repository and collaboration references use canonical
AT URIs and CIDs.

## Publication

Authenticated mutations call the account's PDS through ATProto repository operations.
The application validates values against the Lexicon and verifies returned URI/CID values.
Repository records advertise public HTTPS/SSH/web endpoints; an AT URI identifies the
repository record, while Git remains authoritative for its objects and refs.
Fork records additionally carry an optional `forkedFrom` strong reference to the upstream
repository record. The URI is durable ancestry; the CID records the upstream version seen
when the fork was created. Consumers resolve the current upstream record by URI before a
later sync, so endpoint rotation does not make ancestry stale.

Repository owners publish stable label and milestone definitions and one complete
`dev.adenosine.subjectTriage` snapshot per issue or pull request. The snapshot author must be
the repository's current authority and all referenced definitions must resolve within its
accepted transfer lineage. A former owner's definitions remain readable after transfer, but
the former owner cannot mutate workflow state. See
[`adrs/0002-model-triage-as-owner-authored-snapshots.md`](adrs/0002-model-triage-as-owner-authored-snapshots.md).

A successful publication means the PDS accepted the record. It does not mean this or any
other Adenosine instance has indexed it. Responses return identity needed for client-side
reconciliation. Publication failures are wrapped runtime errors; they do not corrupt local
Git or make local Git reads depend on ATProto availability.

Lexicons are embedded runtime inputs and are maintained by hand. `make generate` does not
currently generate types from them. Tests in the Lexicon and domain packages enforce the
cross-file contract.

## Tap ingestion

Development runs the pinned Tap service with `dev.adenosine.*` collection filters. Tap
delivers a bounded JSON event to the authenticated internal webhook. That endpoint is an
infrastructure boundary, not a public federation API. Adenosine strictly decodes and
validates each untrusted event, including collection-specific authorization rules.

One transaction records the consumer receipt/cursor and updates raw and derived network
state. Duplicate event IDs are no-ops; per-record source event IDs prevent stale events
from replacing newer state. Invalid events with a usable event ID are durably rejected so
they do not block the stream. Target repository owners, rather than arbitrary record
authors, control derived issue and pull request status.

The black-box federation suite uses authenticated Tap-shaped fixtures and a deterministic
publication boundary. It proves projection, isolation, and real Git clone behavior, but
does not claim to run a PDS, Relay, signed event stream, or production Tap deployment. See
[`../test/README.md`](../test/README.md).

## Consistency contract

Clients should show `publishing`, then `indexed`, and after a bounded wait `sync_delayed`.
They should not report a successful publication as failed merely because projection is
late. Every local instance answers discovery from its own PostgreSQL projection, never by
calling the record's originating Adenosine instance during the request.
