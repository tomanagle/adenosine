# Public-Alpha Threat Model

## Scope and assumptions

This model covers the Adenosine API process, native Git HTTP/SSH, PostgreSQL, repository storage, ATProto OAuth/PDS calls, Tap ingestion, Electric proxying, and telemetry export. TLS termination, host hardening, PostgreSQL administration, DNS, backups, and the telemetry backend are operator-managed boundaries.

## Assets

- Session secrets, PATs, OAuth/DPoP credentials, encryption keys, Tap/Electric credentials, SSH host key, and public-key account bindings.
- Private account and moderation data, local repository objects and refs, pull-request state, federation records, and outbox work.
- Authorization integrity: DID ownership, collaborator roles, repository visibility, token scope, and record-author checks.
- Availability of HTTP, SSH, Git subprocess slots, PostgreSQL connections, storage, federation projection, and telemetry queues.

## Trust boundaries

| Boundary | Untrusted side | Trusted side | Principal controls |
| --- | --- | --- | --- |
| HTTP/TLS ingress | browser, API and Git clients | REST/Git handlers | method/route allow-lists, origin/auth checks, body limits, request deadlines |
| SSH ingress | arbitrary network client and command string | SSH parser/authorizer | public-key auth, three tries, exact command grammar, connection/session caps |
| Native Git | protocol bytes, revisions and remote content | one argument-vector runner | no shell, fixed environment, option separators/config, bounded stderr/concurrency/duration, process-group kill |
| Repository storage | logical repository ID | physical filesystem | immutable UUID-derived paths, canonical root, storage package ownership |
| PostgreSQL | validated service input | durable authority | typed queries, transactions, schema separation; statements/parameters excluded from telemetry |
| OAuth/PDS and PR fetch | remote HTTPS and DNS | local credentials/repository | HTTPS validation, public-IP resolution/pinning, redirect denial, expected head/ref checks, bounded responses |
| Tap federation | authenticated but untrusted JSON | network projections | 1 MiB strict decode, Lexicon/domain checks, author checks, transactional receipt/cursor, idempotency |
| Electric | browser shape request | database publication | predefined shapes and server-owned predicates; secret retained server-side |
| Telemetry | application events | Collector/backend | redaction/cardinality contract, bounded batch/queue/retry, no readiness dependency |

## Attackers and abuse cases

- Anonymous internet clients enumerate repositories, flood auth/Git/Tap endpoints, hold streams, exhaust subprocesses/connections, or submit malformed protocol data.
- Authenticated users exceed token scope, access another DID's repository, inject revisions/paths/SSH shell syntax, or publish malicious Markdown and external URLs.
- A malicious remote Git/PDS/DNS endpoint redirects, rebinds, serves oversized data, changes a PR head, or delays indefinitely.
- A compromised Tap source sends replayed, stale, invalid, unauthorized, or adversarial records.
- An operator, dependency, or telemetry backend accidentally leaks secrets through configuration, SQL capture, logs, traces, backups, or dashboards.

## Implemented mitigations

- Git is executed without a shell through one runner. It clears inherited Git/proxy/askpass configuration, uses argument vectors, kills process groups on cancellation, bounds stderr to 32 KiB, limits concurrency to 16, caps admission at five seconds, and caps total duration including admission at 30 minutes.
- SSH accepts only quoted `git-upload-pack` and `git-receive-pack` owner/repository paths, rejects metacharacters and non-session channels, limits auth attempts to three, caps connections/sessions at 128/64, limits handshakes to ten seconds, and closes sessions after two idle minutes.
- Smart HTTP allow-lists routes/protocol versions, requires scoped write authorization, caps upload-pack input at 16 MiB and receive-pack at 2 GiB, and streams pack data.
- Repository physical paths are resolved from immutable IDs by the storage package. Git revisions, refs, pathspecs, outputs, archives, diffs, remote responses, and federation envelopes have dedicated validation or limits.
- Remote PR fetch permits HTTPS, resolves and pins public IPv4/IPv6 addresses, denies loopback/private/link-local addresses under the production policy, disables redirects and interactive credential helpers, and verifies the expected head.
- Sessions/PATs are hashed, OAuth credentials encrypted, PATs scoped/revocable, SSH private keys rejected, and repository authorization is DID-based.
- Tap decoding is strict and bounded; projection uses durable receipts, monotonic event IDs, transactions, record-author checks, local blocks, hidden records/repositories, and reports without changing source records.
- Telemetry uses bounded dimensions, omits SQL text/parameters and content, persists only W3C trace headers, and logs component errors once with trace correlation.

## Operator exceptions

Self-hosters who deliberately allow private remote endpoints must isolate those destinations at the network layer and use a dedicated allow-listing proxy; the default application policy remains public-IP HTTPS only. Operators own TLS, firewalling, an unprivileged UID, read/write directory separation, quotas, database least privilege, secret injection, Collector credentials, retention, backup encryption, and restore testing.

## Residual risks

- No general distributed/per-principal authentication rate limiter exists. SSH attempt caps and finite server resources reduce but do not eliminate credential or connection floods; deploy edge rate limits.
- Push refs can be durable while the subsequent `git.push_received` outbox insert fails. The failure is visible but not automatically reconciled; downstream post-push processing must not be treated as an authoritative record of all pushes.
- Receive-pack permits up to 2 GiB and a command may run for 30 minutes. Quotas, cgroups, process limits, disk monitoring, and edge bandwidth limits remain required.
- Repository deletion is not a staged quarantine workflow. Do not promise immediate or recoverable deletion; operators must handle legal/backup retention separately.
- Webhook delivery is not implemented. There is no webhook URL SSRF boundary, retry queue, signature, deletion event, or backlog metric to audit yet.
- Content is stored as Markdown. Every renderer must sanitize HTML and external resources; current controls do not prove every future client renderer safe.
- Electric/database least-privilege deployment roles are operator configuration and are not automatically conformance-tested.
- Dependency, Git implementation, PostgreSQL, reverse proxy, host, PDS, Tap, and telemetry-backend compromise remain supply-chain/external risks.
- Live DNS rebinding, IPv4/IPv6 transition, sustained-stream, disk exhaustion, moderation leakage, and full cross-system telemetry scenarios need a deployed adversarial environment beyond unit tests.
