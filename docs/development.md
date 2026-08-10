# Development And Testing

## Tool boundary

Use native host Go and Bun for `make test`, `make lint`, and `make generate`. Use Docker
Compose for the local services, `make doctor`, `make shell`, `make psql`, and black-box E2E.
This split keeps normal feedback fast while making stateful environments reproducible.

Required host tools are the Go version in [`../go.mod`](../go.mod), Bun 1.3.13, Make, Git,
Docker with Compose, and curl. Install workspace dependencies with
`bun install --frozen-lockfile`, then start services with `make dev-detached`. The single
development topology is [`../dev/docker-compose.yml`](../dev/docker-compose.yml); startup
preparation belongs in `dev/entrypoint.sh`.

## Backend workflow

1. Read [architecture](architecture.md) and the boundary-specific document.
2. Start PostgreSQL and supporting services with `make dev-detached`; run `make doctor`.
3. Add an immutable migration and query SQL when storage changes.
4. Change the package that owns the behavior, keeping interfaces near consumers and passing context through I/O.
5. Change OpenAPI or a Lexicon first when a public contract changes.
6. Run `make generate` for generated inputs and inspect all output.
7. Add package-local unit tests and the narrowest integration/black-box test that proves the boundary.
8. Run `make test`, `make lint`, and `git diff --check`.
9. Run `make generate` followed by `git diff --exit-code` from a clean generated baseline.
10. Run `make e2e` or `make e2e-federation` when the changed boundary requires it.

## Test layers

- Package unit tests use fakes beside consumers and run under `make test`.
- Database tests exercise SQL/projection behavior against PostgreSQL where configured.
- Real-Git integration tests use actual bare repositories and the native Git executable.
- API tests exercise generated handlers and black-box HTTP contracts.
- `go test -race ./...` is the race layer run by CI and before concurrency-sensitive changes.
- `make e2e` is the single-instance Docker black-box environment.
- `make e2e-federation` is the isolated two-instance projection, real clone, PR, and realtime contract.

Every Go `Test*` function uses a local `testCases` table and executes every case with
`t.Run`, including one-case tests. See [`../AGENTS.md`](../AGENTS.md) and
[`../test/README.md`](../test/README.md) for exact conventions and E2E limitations.

## Generated files

Run `make generate` after changing migrations/query SQL, OpenAPI, or generator
configuration. The command downloads checksum-pinned sqlc into the user cache, invokes the
pinned Go OpenAPI generator, and uses the workspace Hey API generator.

Do not hand-edit:

- `internal/database/generated/` (migrations + query SQL + `sqlc.yaml` inputs)
- `api/generated/go/` (`api/openapi.yaml` + `api/oapi-codegen.yaml` inputs)
- `packages/api-client/src/generated/` (OpenAPI + Hey API config inputs)
- `web/src/routeTree.gen.ts` (TanStack Router route-file input)

Lexicon JSON is an input, not generated output. Generated changes should be deterministic;
CI runs `make generate && git diff --exit-code`.
