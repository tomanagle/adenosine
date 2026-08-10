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

Run `make generate` after changing generated API, database, or Lexicon inputs. The script caches a checksum-verified sqlc release and invokes the pinned OpenAPI generator through `go run`; separate global installs are not required.

## Engineering rules

- Use explicit constructor injection. Do not introduce service locators or mutable dependency globals.
- Define narrow interfaces near their consumers.
- Propagate `context.Context` through request and I/O boundaries.
- Wrap errors with useful operation context and preserve `errors.Is` and `errors.As` behavior.
- Use structured `slog` logging and never log credentials, request bodies, or Git pack data.
- Do not execute dynamic Git or SSH commands through a shell.
- Keep public wire models at the transport edge.
- Format code with `gofmt` and add tests for behavior changes.

The complete architectural constraints and implementation order are documented in [`plan.md`](plan.md).
