# Adenosine Web

The first-party TanStack Start client consumes the documented Adenosine REST and
Electric sync interfaces through the `@adenosine/api-client` workspace package.

Only `/` is server rendered. `/home` and `/login` are browser-owned routes. The
home route uses TanStack Query for its identity and REST snapshot, then starts a
route-scoped TanStack DB/Electric repository collection for optional live data.

From the workspace root, install dependencies with `bun install`. In this
directory use `bun run dev`, `bun run typecheck`, `bun run test`, and
`bun run build`. The production server starts with `bun run start`.

See [`../plans/web-ui.md`](../plans/web-ui.md) for the full architecture and
delivery plan.
