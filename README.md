# Adenosine

Adenosine is a federated, self-hostable Git forge built around standard Git protocols and AT Protocol identity and collaboration records.

It combines a Go API and Git server, PostgreSQL projections, AT Protocol federation, an optional Electric realtime read path, and a TanStack Start web client. REST is the canonical write interface and is documented through OpenAPI.

## Features

- Native Git Smart HTTP and SSH clone, fetch, and push against sharded bare repositories
- AT Protocol OAuth, browser sessions, passkeys, personal access tokens, and SSH keys
- Public and private repositories with owner and collaborator authorization
- Repository tree, blob, branch, tag, commit, history, merge-base, and bounded diff APIs
- Federated profiles, repositories, stars, issues, comments, moderation, and pull requests
- Secure cross-instance pull request fetches, reviews, merge commits, and squash merges
- PostgreSQL outbox events, Tap ingestion, Electric repository sync, and OpenTelemetry export
- Generated Go server bindings and a public TypeScript API client

## Development

Requirements:

- Go 1.24.3 or the version declared by `go.mod`
- Bun 1.3.13
- Docker with Docker Compose
- Make
- Git

Start the development stack:

```sh
make dev
```

Docker is used to run the local service stack and E2E environments, not for normal tests, linting, or generation. `make dev` creates an ignored `.env.local` with development credentials when needed. The complete container setup is in [`dev/docker-compose.yml`](dev/docker-compose.yml); container startup prepares the database roles, module cache, repository directory, and persistent SSH host key.

Run it in the background with `make dev-detached`, inspect it with `make logs`, and stop it with `make down`.

The generated development defaults are:

- Web UI and API gateway: <http://127.0.0.1:8080>
- Git SSH: `ssh://git@localhost:2222/<owner>/<repository>.git`
- Liveness: <http://127.0.0.1:8080/health/live>
- Readiness: <http://127.0.0.1:8080/health/ready>
- API documentation: <http://127.0.0.1:8080/docs/api>
- OpenAPI: <http://127.0.0.1:8080/openapi.json>
- Grafana: <http://localhost:3001>
- PostgreSQL: `localhost:5432`

Your `.env.local` is the source of truth when ports are overridden. Electric is intentionally internal and is exposed to clients only through documented API sync routes.

## Development Commands

```sh
make test            # Native Go and frontend tests
make lint            # Native Go checks and frontend type checking
make generate        # Native pinned sqlc, OpenAPI, and client generation
make e2e             # local stack acceptance
make e2e-federation  # isolated two-instance federation acceptance
make doctor          # health-check a running development stack
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md), [`docs/api-authentication.md`](docs/api-authentication.md), [`docs/git-http.md`](docs/git-http.md), [`docs/git-ssh.md`](docs/git-ssh.md), [`docs/repository-browser.md`](docs/repository-browser.md), and [`plan.md`](plan.md) for the development workflow, protocol details, and architecture.

## Status

Adenosine is under active development and is not ready for production use. The Git, identity, federation, collaboration, pull request, public API, and realtime foundations are implemented and covered by isolated federation acceptance tests. The first-party web client currently provides the initial landing, login, identity, and live repository surfaces; the complete forge UI and production deployment hardening remain in progress.

## License

Licensed under the Apache License, Version 2.0. See [`LICENSE`](LICENSE).
