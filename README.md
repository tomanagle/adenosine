# Adenosine

Adenosine is a federated, self-hostable Git forge built around standard Git protocols and AT Protocol identity and collaboration records.

The project is in its initial implementation phase. The current foundation provides PostgreSQL-backed repository metadata, sharded bare Git storage, Smart HTTP and SSH clone/push transports, local access credentials and permissions, repository tree/blob/history/diff APIs, durable post-push events, OpenTelemetry export, and a Docker-first development environment.

## Development

Requirements:

- Docker with Docker Compose
- Make
- Git

Start the development stack:

```sh
make dev
```

The complete development container setup is in [`dev/docker-compose.yml`](dev/docker-compose.yml). The Adenosine container entrypoint prepares its module cache, repository directory, and persistent development SSH host key before starting the requested command.

Run it in the background with `make dev-detached`, inspect it with `make logs`, and stop it with `make down`.

Once ready:

- API: <http://localhost:8080>
- Git SSH: `ssh://git@localhost:2222/<owner>/<repository>.git`
- Liveness: <http://localhost:8080/health/live>
- Readiness: <http://localhost:8080/health/ready>
- API documentation: <http://localhost:8080/docs/api>
- OpenAPI: <http://localhost:8080/openapi.json>
- Grafana: <http://localhost:3001>
- PostgreSQL: `localhost:5432`

Run `make doctor` to validate the local environment and running services. See [`docs/api-authentication.md`](docs/api-authentication.md), [`docs/git-http.md`](docs/git-http.md), [`docs/git-ssh.md`](docs/git-ssh.md), [`docs/repository-browser.md`](docs/repository-browser.md), and [`plan.md`](plan.md) for authentication, transport and read API details, and the implementation architecture.

## Status

Adenosine is not ready for production use. Local Git hosting, credential management, and repository read APIs are in place; ATProto OAuth session issuance, the web UI, and federation remain under development.

## License

Licensed under the Apache License, Version 2.0. See [`LICENSE`](LICENSE).
