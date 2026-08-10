# Adenosine Web

The first-party TanStack Start client consumes the documented Adenosine REST and
Electric sync interfaces through the `@adenosine/api-client` workspace package.

The root route ensures identity once and passes it through typed router context
to every child route. `/` renders the public landing page anonymously and the
personal home page when authenticated; `/login` redirects authenticated users
back to `/`. TanStack Query owns the identity and REST snapshot caches, while
route-scoped TanStack DB/Electric collections provide optional live data. Those
collections use fixed, same-origin public sync resources, on-demand POST subsets,
and are disposed when their owning route scope unmounts or changes.
Electric remains an optional read path; REST owns writes and all Git-object
reads, and collection failures do not replace the Query snapshot.

Route activity is composed and bounded from those explicit collections; there
is no catch-all activity or `network.records` sync resource. ATProto mutation
consumers can use the shared URI/CID reconciler to distinguish `publishing`,
`indexed`, and `sync_delayed` without treating delayed indexing as write
failure.

From the workspace root, install dependencies with `bun install`. In this
directory use `bun run dev`, `bun run typecheck`, `bun run test`, and
`bun run build`. The production server starts with `bun run start`.

See [`../plans/web-ui.md`](../plans/web-ui.md) for the full architecture and
delivery plan.
