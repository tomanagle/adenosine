# Contributing

## Local workflow

Install the Go version declared by `go.mod` and Bun 1.3.13, then install the workspace dependencies:

```sh
bun install --frozen-lockfile
```

Tests, linting, type checking, and generation run directly on the host. Docker is reserved for the local service stack and black-box environments:

```sh
make dev-detached
make test
make lint
make doctor
```

For a backend change, start with the [documentation index](docs/README.md), then use the
source-of-truth checklist in [database and projections](docs/database.md) or
[REST API](docs/api.md). Package-local tests should prove domain behavior; add the narrowest
real-database, real-Git, HTTP, or federation layer needed to prove the changed boundary.

Run `make generate` after changing migrations/query SQL, OpenAPI, or generator
configuration. The script caches a checksum-verified sqlc release, invokes the pinned
OpenAPI generator through `go run`, and runs the workspace TypeScript client generator;
separate global generator installs are not required. Do not hand-edit
`internal/database/generated/`, `api/generated/go/`,
`packages/api-client/src/generated/`, or `web/src/routeTree.gen.ts`.

Oxlint and Oxfmt are pinned workspace dependencies; global installs are not needed. `make lint` runs
their non-mutating checks. Use `bun run lint:fix` and `bun run format` to fix web code locally, or run
`bun run lint` and `bun run format:check` independently. Editor integrations should use the workspace
`oxlint` and `oxfmt` executables and enable format on save with Oxfmt.

## Engineering rules

- Use explicit constructor injection. Do not introduce service locators or mutable dependency globals.
- Define narrow interfaces near their consumers.
- Propagate `context.Context` through request and I/O boundaries.
- Wrap errors with useful operation context and preserve `errors.Is` and `errors.As` behavior.
- Use structured `slog` logging and never log credentials, request bodies, or Git pack data.
- Do not execute dynamic Git or SSH commands through a shell.
- Keep public wire models at the transport edge.
- Format code with `gofmt` and add tests for behavior changes.
- Keep startup fail-fast behavior in package-owned `Must` functions; runtime paths return wrapped errors.
- Treat Git refs/objects, DIDs, AT URIs/CIDs, local authoritative tables, and network projections according to [architecture](docs/architecture.md).

Before submitting a change, run `make test`, `make lint`, `make generate`, verify generation
did not leave an unexpected diff, and run `git diff --check`. Use `make e2e` or
`make e2e-federation` for affected black-box boundaries. Detailed test layers and generated
ownership are in [development and testing](docs/development.md). `plan.md` is roadmap and
design history; current docs and code define implemented behavior.
