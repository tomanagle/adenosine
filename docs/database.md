# Database And Projections

## Schema ownership

Ordered SQL files in [`../migrations/`](../migrations/) are the database schema source of
truth and are embedded in the binary. The schemas have distinct ownership:

- `core.*` is authoritative local account, repository, alias, and collaborator state.
- `auth.*` contains local credentials and ceremonies. Treat dumps as secrets.
- `network.*` is rebuildable, eventually consistent ATProto-derived data.
- `ops.*` stores delivery cursors, receipts, and durable local events.
- `moderation.*` stores private local viewing policy and is not federated.

Add an ordered migration for every schema change. Applied migration files are immutable:
never rewrite one that may have shipped. Startup applies each pending file in its own
transaction under a PostgreSQL advisory lock and records it in
`public.schema_migrations`. There is no automatic down migration.

## sqlc workflow

SQL queries in [`../internal/database/queries/`](../internal/database/queries/) and the
migrations are sqlc inputs. [`../sqlc.yaml`](../sqlc.yaml) generates
`internal/database/generated/`. Change SQL inputs, run `make generate`, then adapt the
package-owned store. Do not hand-edit generated sqlc output.

Application packages own projection semantics and stores; generated query code only owns
typed database access. Keep transaction boundaries around invariants that must move
together, such as a Tap receipt, cursor, raw record, derived row, and counters. Use
event/CID guards in SQL so duplicates and stale create/update/delete events cannot regress
state.

## Replay and recovery

`network.*` is derived from validated Tap events and can be rebuilt by replaying the
authoritative ATProto stream from an appropriate cursor into an empty projection. The
current repository does not ship an operator-facing replay command. During development,
`make reset` destroys the local Compose data and recreates it; it is not a production
restore procedure.

Do not treat replay as a backup for `core.*`, `auth.*`, `ops.*`, moderation preferences,
or Git repositories. See [self-hosting](self-hosting.md) for the current backup gap.

## Backend change checklist

1. Add a new migration; do not modify an applied one.
2. Update or add query SQL and run `make generate`.
3. Implement behavior in the owning package through a narrow interface.
4. Test state transitions, duplicate delivery, stale delivery, and transaction rollback as applicable.
5. Update `api/openapi.yaml` or Lexicons when a public contract changes and regenerate.
6. Run the test layers in [development](development.md).
