# Agent Instructions

## Startup Composition

Keep `cmd/adenosine/main.go` minimal. Packages that construct required startup values own their fail-fast behavior through a public `Must` function and a private error-returning implementation.

Use this pattern:

```go
func Must() Config {
	value, err := load()
	if err != nil {
		panic(err)
	}
	return value
}

func load() (Config, error) {
	// Parse, validate, and return contextual errors.
}
```

For constructors with inputs:

```go
func Must(ctx context.Context, cfg config.Config) *app.Application {
	value, err := build(ctx, cfg)
	if err != nil {
		panic(err)
	}
	return value
}
```

The composition root should read plainly:

```go
cfg := config.Must()
application := di.Must(ctx, cfg)
```

Do not add generic `must[T]` helpers in `main`, repeated startup error branches, or exported error-returning constructors when the application cannot run without the result. Keep `panic` restricted to these startup-only `Must` functions. Runtime and request-path failures must continue to return normal wrapped errors.

## Development Environment

Development Docker files belong under `dev/`. Use the single `dev/docker-compose.yml`; do not split development configuration across Compose override files.

Container-local startup preparation belongs in `dev/entrypoint.sh`. Do not add a separate bootstrap container for work that is part of the Adenosine container's startup lifecycle.

Use Docker for the local service stack and black-box environments: `make dev`, `make dev-detached`, `make e2e`, and `make e2e-federation`. Run unit tests, linting, type checking, and code generation with the host Go and Bun toolchains through `make test`, `make lint`, and `make generate`; do not wrap those targets in Docker.

## Go Tests

Every `Test*` function must use a local table named `testCases` and execute every entry with `t.Run`, including tests that currently have only one case. Put case-specific inputs, dependency behavior, and expected results in the table so new scenarios can be added without restructuring the test body.

Use this shape:

```go
func TestExample(t *testing.T) {
	testCases := []struct {
		name string
		want string
	}{
		{name: "success", want: "value"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Arrange, act, and assert using testCase fields.
		})
	}
}
```

Prefer concrete expectation fields such as `wantErr error` over generic booleans when error identity matters. Keep shared arrange/act/assert logic in the loop body; if cases need substantially different control flow, split the behavior into separate `Test*` functions and give each one its own `testCases` table.

Keep small handwritten fakes and spies beside their consumer tests by default. Promote a test double into shared test support only when multiple files need the same stable behavior. Use generated mocks selectively for large interfaces or meaningful interaction ordering, not as the default replacement for state-based assertions.
