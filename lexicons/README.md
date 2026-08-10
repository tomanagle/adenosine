# Adenosine Lexicons

This directory contains the implementation-independent `dev.adenosine.*` AT Protocol Lexicons.

`dev.adenosine.profile` is a singleton `self` record containing public developer-network metadata. DID and handle are derived from AT Protocol identity and are not duplicated in the record.

The JSON documents are hand-maintained public contract inputs and are embedded by
`embed.go`; they are not generated output. Changes must preserve canonical AT URI/CID
strong references and collection-specific author rules, update the matching Go validation
and projection behavior, and pass `make test`. See
[`../docs/federation.md`](../docs/federation.md) for publication, Tap ingestion, replay,
and eventual-consistency behavior.
