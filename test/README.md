# Black-box tests

Cross-package contract, Git transport, federation, and realtime tests belong here. Package-local unit tests stay beside their Go source.

`make test` runs native Go unit/integration tests, offline documentation/tooling tests, and
web tests. Database and real-Git integration tests are distinct from generated-handler API
tests. CI additionally runs `go test -race ./...`. Docker is used for `make e2e` and
`make e2e-federation`; see [`../docs/development.md`](../docs/development.md) for when to
choose each layer.

## Two-instance projection and clone contract

`make e2e-federation` runs the isolated Step 24 acceptance topology under the Compose `federation-test` profile. It starts independent A and B applications with separate Postgres databases, repository storage, instance state, tmpfs mounts, and private networks. Instance B has no network path to A's API, TLS proxy, or database.

The A-side fixture host runs inside `adenosine-a` and uses the production database, account store, repository service, filesystem, endpoint builder, and native Git service to create a genuine active public repository with an initial README commit. Only the unavailable OAuth/PDS publication boundary is replaced by a deterministic publisher. A profile-only pinned Caddy proxy gives that repository a production-valid `https://adenosine-a-tls` URL using Caddy's internal CA. The acceptance runner mounts that CA read-only and configures Git with `sslCAInfo`; TLS verification is never disabled.

The test submits authenticated, valid Tap identity and Adenosine record webhook fixtures directly to each instance. It delivers the hosted repository's matching portable record only to B, discovers the exact HTTPS Git URL from B by AT URI and CID, clones it with vanilla Git, and verifies the README and origin URL. It also proves A-only and B-only projection isolation, common-record visibility, duplicate-delivery idempotency, cursor pagination, and a `400` response for a malformed cursor. Step 28 coverage delivers target-owner status and common-DID review records before their Bob-authored pull request, verifies both PostgreSQL projections and counters directly, and proves a malicious Bob-authored target status remains raw data without controlling derived state. The transfer phase then runs the production transfer service and PostgreSQL store on A with deterministic publication boundaries, proves the repository UUID and storage key remain stable, clones through both old and new HTTPS routes, and delivers the bilateral records to A and B in different orders before checking canonical convergence. Instance A and Postgres A are stopped before the final B projection queries.

This is intentionally a **two-instance authenticated Tap webhook projection and real Git clone contract**. It does not run real OAuth, a PDS, or a Relay, does not validate signed ATProto events, and must not be described as proof of PDS/Relay federation.

The target is self-contained, uses a unique Compose project, runs its black-box harnesses
inside the project development image, and always removes its containers, networks, and
volumes. Normal `make dev` behavior is unchanged because the acceptance services are
profile-only. This is a deliberate exception to native host Go for normal `make test` and
`make lint`: black-box E2E belongs in Docker.

## Realtime federation contract

The same target also runs the issue #3 realtime phase with independent `electric-a` and `electric-b` services, secrets, replication roles, publications, and persistent storage. PostgreSQL A and B use logical WAL settings, and each Adenosine entrypoint performs the same least-privilege Electric role and publication preparation used by development. Electric starts after the application has prepared PostgreSQL, but neither application health nor REST readiness depends on Electric.

The A-only producer calls the real star application service against A's local repository projection. Its deterministic publisher replaces the unavailable authenticated PDS write and emits a stable Tap-shaped fixture to the named `realtime-boundary` service. That boundary is the only test component holding B's Tap webhook credential and forwards the fixture to B's authenticated `/internal/federation/tap` endpoint. This proves the application-service-to-validated-Tap-projection contract across a deterministic test boundary; it does **not** run or prove a production PDS, Relay, signed event stream, or Tap server.

The observer has one internal network attachment shared only with `realtime-sync-gateway`. That gateway accepts only anonymous `GET /api/v1/sync/stars` and can reach only B's HTTP network; the observer has no route or credentials for A, either PostgreSQL database, either Electric service, the publication boundary, or B's internal webhook. After completing the initial shape, the observer starts a `live=true` continuation. The harness waits for the gateway's upstream `WroteRequest` handshake before triggering publication, so the change must arrive on the already-written request rather than through polling or reload. The observer then reconnects from the returned `Electric-Handle` and `Electric-Offset` before observing deletion on another already-written request.

The realtime fixture delivers create replay, a lower-ID stale delete, and delete replay. It asserts a live create and delete, continuation resume, and that every row field is in the documented seven-column `SyncStar` projection. A is stopped before delete reaches B, proving B serves its local projection without request-time A access. Finally, the harness stops `electric-b` and compares representative B network-discovery REST reads before and after the failure. All waits are bounded, and failure output includes service state plus PostgreSQL, application, Electric, boundary, gateway, and observer logs.
