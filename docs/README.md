# Documentation

Start with the document for the boundary you are changing:

- [Architecture and sources of truth](architecture.md)
- [Database, migrations, sqlc, and projections](database.md)
- [REST API and versioning](api.md)
- [Public owner routing](owner-routing.md)
- [API authentication](api-authentication.md)
- [Lexicons, publication, and Tap](federation.md)
- [Realtime and REST fallback](realtime.md)
- [Git Smart HTTP](git-http.md), [Git SSH](git-ssh.md), and [pull request security](pull-requests.md)
- [Repository read API](repository-browser.md)
- [Forks and upstream synchronization](forks.md)
- [Observability](observability.md)
- [Development and test layers](development.md)
- [Security model](security.md)
- [Self-hosting, backups, upgrades, and deployment status](self-hosting.md)

[`../api/openapi.yaml`](../api/openapi.yaml) is the installed REST contract,
[`../lexicons/`](../lexicons/) contains the federation schemas, and
[`../migrations/`](../migrations/) defines PostgreSQL storage. [`../plan.md`](../plan.md)
and [`../plans/`](../plans/) are design history and future plans, not statements that a
feature or deployment target is available.
