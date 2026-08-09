# Contributing

## Local workflow

Use the Dockerized project toolchain so local Go and PostgreSQL versions do not affect results:

```sh
make dev-detached
make test
make lint
make doctor
```

Run `make generate` after changing generated API, database, or Lexicon inputs once those generators are introduced.

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
