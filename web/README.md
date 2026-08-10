# Adenosine Web

The first-party TanStack Start client consumes the documented Adenosine REST and
Electric sync interfaces through the `@adenosine/api-client` workspace package.

The root route ensures identity once and passes it through typed router context
to every child route. `/` renders the public landing page anonymously and the
personal home page when authenticated; `/login` redirects authenticated users
back to `/`. TanStack Query owns the identity and REST snapshot caches, while
route-scoped TanStack DB/Electric collections provide optional live data.

From the workspace root, install dependencies with `bun install`. In this
directory use `bun run dev`, `bun run typecheck`, `bun run test`, and
`bun run build`. The production server starts with `bun run start`.

See [`../plans/web-ui.md`](../plans/web-ui.md) for the full architecture and
delivery plan.
