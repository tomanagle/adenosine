# Black-box tests

Cross-package contract, Git transport, federation, and realtime tests belong here. Package-local unit tests stay beside their Go source.

## Two-instance projection and clone contract

`make e2e-federation` runs the isolated Step 24 acceptance topology under the Compose `federation-test` profile. It starts independent A and B applications with separate Postgres databases, repository storage, instance state, tmpfs mounts, and private networks. Instance B has no network path to A's API, TLS proxy, or database.

The A-side fixture host runs inside `adenosine-a` and uses the production database, account store, repository service, filesystem, endpoint builder, and native Git service to create a genuine active public repository with an initial README commit. Only the unavailable OAuth/PDS publication boundary is replaced by a deterministic publisher. A profile-only pinned Caddy proxy gives that repository a production-valid `https://adenosine-a-tls` URL using Caddy's internal CA. The acceptance runner mounts that CA read-only and configures Git with `sslCAInfo`; TLS verification is never disabled.

The test submits authenticated, valid Tap identity and Adenosine record webhook fixtures directly to each instance. It delivers the hosted repository's matching portable record only to B, discovers the exact HTTPS Git URL from B by AT URI and CID, clones it with vanilla Git, and verifies the README and origin URL. It also proves A-only and B-only projection isolation, common-record visibility, duplicate-delivery idempotency, cursor pagination, and a `400` response for a malformed cursor. Step 28 coverage delivers target-owner status and common-DID review records before their Bob-authored pull request, verifies both PostgreSQL projections and counters directly, and proves a malicious Bob-authored target status remains raw data without controlling derived state. Instance A and Postgres A are stopped before the final B projection queries.

This is intentionally a **two-instance authenticated Tap webhook projection and real Git clone contract**. It does not run real OAuth, a PDS, or a Relay, does not validate signed ATProto events, and must not be described as proof of PDS/Relay federation.

The target is self-contained, uses a unique Compose project, runs Go inside the project development image, and always removes its containers, networks, and volumes. Normal `make dev` behavior is unchanged because the acceptance services are profile-only.
