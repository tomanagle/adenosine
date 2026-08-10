# Realtime

Electric is an optional delivery mechanism for safe PostgreSQL projections. It is not a
write path, source of truth, or direct browser-to-database connection. The Go API selects
the table/projection, columns, visibility and moderation predicate, and queryable columns,
then proxies the Electric shape protocol through documented sync operations such as
`/api/v1/sync/repositories`.
Credentials and the Electric secret stay server-side.

Both GET continuation and POST subset requests are available as declared by the installed
OpenAPI contract. POST predicates can only narrow the server-owned shape. The proxy
streams responses, preserves Electric continuation headers, bounds request bodies, and
does not expose raw `network.records`, auth data, or moderation tables.

Anonymous and PAT requests receive public shapes. A valid browser session applies that
account's private moderation policy, so those responses are private and non-cacheable. An
invalid presented session is rejected, not silently downgraded. After moderation changes,
restart from `offset=-1` when instructed by the OpenAPI contract.

## Client behavior

1. Load a normal REST snapshot.
2. Start only the route-scoped collections needed by the mounted UI.
3. Reconcile ATProto mutations by returned URI/CID, not by a guessed PostgreSQL transaction.
4. Dispose collections when their route scope changes.
5. If Electric fails, retain the snapshot, show that live updates are unavailable, and use REST refetching and mutations.

Application readiness intentionally does not depend on Electric. The black-box realtime
suite proves an already-open live request, continuation resume, create/delete replay and
stale guards, instance isolation, and unchanged REST discovery while Electric is stopped.
Its exact boundary and limitations are documented in [`../test/README.md`](../test/README.md).
