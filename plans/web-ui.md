# Adenosine Web UI Plan

> A public, API-first Git forge UI built with TanStack Start, TanStack Router,
> TanStack Query, TanStack DB, and Electric.

## 1. Recommendation

Build `web/` as a standalone TanStack Start application that consumes only:

```text
api/openapi.yaml-generated REST SDK
documented /api/v1/sync/* Electric shape endpoints
browser AT Protocol OAuth redirects
```

Use the technologies for distinct jobs:

```text
TanStack Start   routing, full-document SSR, streaming, deployment
TanStack Router  typed route params, search state, loaders, boundaries
TanStack Query   REST reads, SSR hydration, Git data, mutations
TanStack DB      normalized client projections and live queries
Electric         realtime delivery of safe PostgreSQL projections
Tailwind CSS     layout and utility styling
shadcn/ui        accessible application components owned in the repo
OpenAPI          public REST and sync-row contract
Hey API          generated TypeScript SDK, Query options, and validators
```

REST remains authoritative for all writes. Electric is an optional synchronized
read path. The first-party UI must continue to work when Electric is unavailable.

## 2. UI foundation

Keep the first UI intentionally simple and functional. Use shadcn/ui components
with Tailwind CSS v4 and minimal visual customization.

Initialize shadcn/ui in the existing `web/` workspace with:

```text
TanStack Start template
Tailwind CSS v4 through the Vite plugin
new-york style
neutral base color
CSS variables enabled
@/* import alias
```

Use the stock semantic tokens for the initial light and dark themes. Do not build
a custom component system, bespoke animation language, or brand treatment before
the main product flows are working.

Start with only the shadcn/ui components required by the first slices:

```text
button, input, textarea, label
card, badge, separator, skeleton
tabs, breadcrumb, dropdown-menu, select
dialog, sheet, tooltip, popover
table, scroll-area, pagination
alert, sonner
```

The component source belongs in `web/src/components/ui/` and may be changed when
the product needs it. Feature code should compose those components instead of
copying their classes or creating competing primitives.

Interaction priorities remain:

1. Code and collaboration content are primary; federation metadata is contextual.
2. Local and remote repositories use the same layout.
3. Remote-host actions explain where the action will occur before navigation.
4. Live updates preserve scroll position.
5. Optimistic records distinguish `publishing`, `indexed`, and `delayed`.
6. Important actions work with keyboard navigation and visible focus.

## 3. Information architecture

### Global product areas

```text
Home / network activity
Explore repositories and developers
Repository
  Code
  Commits
  Branches / tags
  Issues
  Pull requests
  Settings (local repositories only, authorized users only)
Developer profile
Create repository
Account settings
  Profile
  Passkeys
  SSH keys
  Access tokens
  Moderation
```

### Proposed routes and rendering policy

Only the anonymous landing page is server rendered. The authenticated product is
a client-rendered application below the shared TanStack Start document shell.
`ssr: true` means data and markup render on the server; `false` means the route's
loader and component run in the browser.

| Route | Purpose | SSR | Initial data | Live data |
|---|---|---:|---|---|
| `/` | Anonymous public landing; authenticated users redirect to `/home` | true | identity check + public REST | none |
| `/home` | Signed-in personal homepage | false | REST/Query | activity/repository/profile collections |
| `/explore` | Network repository discovery | false | REST/Query | repository/profile collections |
| `/login` | ATProto and passkey entry | false | identity query | none |
| `/profiles/$identity` | Portable developer profile | false | REST/Query | profile/repository collections |
| `/$owner/$repo` | Repository overview and README | false | REST/Query | repository/star collection |
| `/$owner/$repo/tree/$` | Tree and source browser | false | REST/Query | branch metadata only |
| `/$owner/$repo/blob/$` | File view | false | REST/Query | none |
| `/$owner/$repo/commits` | Commit history | false | REST/Query | none |
| `/$owner/$repo/commit/$revision` | Commit and bounded diff | false | REST/Query | none |
| `/$owner/$repo/compare` | Interactive comparison | false | REST/Query | none |
| `/$owner/$repo/issues` | Issue list | false | REST fallback | issues/profiles collections |
| `/$owner/$repo/issues/$issue` | Issue and comments | false | REST fallback | issue/comment/profile collections |
| `/$owner/$repo/pulls` | Pull request list | false | REST fallback | PR/profile collections |
| `/$owner/$repo/pulls/$pull` | PR review and diff | false | REST/Query | PR/review/profile collections |
| `/$owner/$repo/settings` | Local repository administration | false | REST/Query | none initially |
| `/new` | Create a local repository | false | identity query | none |
| `/settings/*` | Account and credential management | false | REST/Query | none |

On an initial `/` request, the server uses the request-scoped generated SDK to
check the existing Adenosine session cookie. An unauthenticated request renders
the simple public landing page. An authenticated request redirects to `/home`
before rendering landing-page markup. `/home` uses `ssr: false` and starts the
personal Query/DB data flow in the browser.

This split keeps personalized data out of server-rendered HTML and removes any
need to combine SSR and TanStack DB live-query rendering. API authorization still
remains authoritative for every personal read and mutation.

## 4. Repository page composition

The repository page is the main product surface.

```text
global header
  instance identity + global search + create + account

repository masthead
  owner / repository
  visibility + local/remote hosting badge
  star control + clone control
  origin rail

repository navigation
  Code | Issues | Pull requests | Activity

route content
  branch selector + breadcrumb + file actions
  tree / blob / README / commit / issue / PR content

context rail (wide screens only)
  description, host, clone URLs, counts, latest activity
```

The remote repository state must be explicit:

- “Hosted here” for local Git storage.
- “Hosted by `code.example`” for a remote repository.
- Clone URLs always come from the API `hosting` object.
- Unsupported host-local actions are disabled with an explanation and an
  outbound link, not silently hidden.

## 5. Application structure

Keep route files thin. Features own their queries, collections, mutations, and
presentation components.

```text
web/src/
├── routes/
│   ├── __root.tsx
│   ├── index.tsx
│   ├── home.tsx
│   ├── explore.tsx
│   ├── login.tsx
│   ├── profiles.$identity.tsx
│   ├── $owner.$repo.tsx
│   ├── $owner.$repo.tree.$.tsx
│   ├── $owner.$repo.commits.tsx
│   ├── $owner.$repo.issues.tsx
│   ├── $owner.$repo.issues.$issue.tsx
│   └── settings.*
├── features/
│   ├── identity/
│   ├── profiles/
│   ├── repositories/
│   ├── code-browser/
│   ├── issues/
│   ├── pull-requests/
│   └── settings/
├── api/
│   ├── browser-client.ts
│   ├── server-client.ts
│   └── errors.ts
├── db/
│   ├── collection-factories.ts
│   ├── live-projection.tsx
│   └── collections/
├── components/
│   ├── ui/                    # shadcn/ui-owned source
│   └── app/                   # shared Adenosine compositions
├── lib/
│   └── utils.ts               # shadcn cn() utility
├── styles/
│   └── globals.css            # Tailwind + shadcn semantic tokens
├── router.tsx
└── start.ts
```

Client-rendered product route loaders import generated `queryOptions` and call
`queryClient.ensureQueryData`. Components consume the same options through
`useSuspenseQuery`, giving navigation, preloading, and cache identity one shared
definition. The anonymous `/` route uses a request-scoped generated SDK client
only for its server-side session check.

## 6. Data ownership

### TanStack Query

Use Query for request/response resources:

```text
current identity
repository tree and blob reads
branches and tags
commit history and commit details
diffs and merge-base
credential and settings pages
REST fallback reads
all REST mutation requests
```

Git object data stays out of TanStack DB and PostgreSQL projections.

### TanStack DB and Electric

Use typed collections for PostgreSQL-backed projections:

```text
profiles
repositories
stars
issues
issue comments
pull requests
pull request reviews
later: notifications, activity, branch projection metadata
```

Use live queries for client-side joins and views such as:

```text
repository + owner profile
issue + author profile + current status
comment thread + author profiles
pull request + reviews + reviewer profiles
filtered/sorted explore results
```

Do not eagerly sync the entire public network. Start with route-scoped collection
factories, then adopt on-demand collection loading for discovery and search after
it is validated against the pinned TanStack DB/Electric versions.

### URL and local UI state

Keep shareable state in validated Router search parameters:

```text
branch/ref
tree path
issue state filter
sort order
search query
diff whitespace mode
```

Keep ephemeral state local:

```text
open clone menu
focused diff line
composer draft before persistence is added
```

## 7. Rendering and live-query boundary

There is deliberately no SSR-to-live handoff in the initial application.

```text
GET /
  -> Start runs the server-side identity check through @adenosine/api-client
  -> unauthenticated: SSR the public landing page
  -> authenticated: redirect to /home before rendering

GET /home
  -> Start returns the client-route fallback because ssr: false
  -> browser loads identity and personal REST snapshot with Query
  -> browser starts route-scoped Electric collections
  -> TanStack DB useLiveQuery renders personal activity
```

This avoids the current TanStack DB SSR limitation entirely. As of August 2026,
released TanStack DB versions do not provide a stable live-query SSR hydration
contract: the open report documents a missing `getServerSnapshot`, and its
replacement SSR implementation remains a draft pull request.

Rules for the initial implementation:

- `/` never instantiates Electric or calls `useLiveQuery`.
- `/home` and the rest of the product routes use `ssr: false`.
- Construct route-scoped Electric collections in browser-owned feature modules,
  never at SSR module scope.
- Bootstrap personal routes with a normal Query REST snapshot so they remain
  usable while Electric connects.
- If Electric fails or times out, retain the Query snapshot, show a quiet “Live
  updates unavailable” status, and allow normal Query refetching and mutations.
- If an SSR route later needs live data, isolate the live region behind TanStack
  Start `ClientOnly`; do not call `useLiveQuery` during server rendering until
  native TanStack DB SSR support is released and verified.

## 8. Electric sync contract

Expose Electric through the Go public API, not through a privileged frontend-only
TanStack Start server route:

```text
GET|POST /api/v1/sync/repositories
GET|POST /api/v1/sync/profiles
GET|POST /api/v1/sync/issues
GET|POST /api/v1/sync/issue-comments
GET|POST /api/v1/sync/pull-requests
GET|POST /api/v1/sync/pull-request-reviews
GET|POST /api/v1/sync/stars
```

Support both GET and POST from the first release and configure Electric
collections with `subsetMethod: "POST"`. Electric's current guidance says POST
will become the required subset-query method in Electric 2.0.

For every endpoint the server must:

1. choose the table or safe projection itself;
2. set the allowed columns and queryable columns itself;
3. apply the main visibility/moderation predicate itself;
4. forward only Electric protocol continuation parameters;
5. allow client subset predicates only to narrow the server-owned shape;
6. keep the Electric secret and database credentials server-side;
7. stream the upstream response without buffering it;
8. remove stale `content-encoding` and `content-length` headers after fetch
   decompression;
9. give Electric a read role limited to explicit sync tables/projections.

If the public REST model is nested but the Electric row is flat, define a separate
public `Sync*` schema. Never pretend the database row and REST resource are the
same type when they are not.

## 9. Docker Compose topology

The UI and Electric must run in the existing single `dev/docker-compose.yml`.
Do not add a Compose override or a bootstrap container. Add `web`, `electric`,
and `gateway` to the default development stack:

```text
browser
  -> gateway:8080
       -> page routes, assets, HMR -> web:3000
       -> /api, /oauth, /health, API docs, Git smart HTTP -> adenosine:8080

web SSR
  -> adenosine:8080 REST API

browser live collection
  -> gateway:8080/api/v1/sync/*
       -> adenosine sync proxy
            -> electric:3000
                 -> postgres:5432 logical replication
```

The gateway keeps UI, API, OAuth, cookies, CSRF origin checks, and Git HTTP on
the existing public `ADENOSINE_BASE_URL`. Only the gateway owns host port 8080;
the Go and web HTTP ports remain internal. SSH remains exposed on 2222, while
Postgres and Grafana retain their current development ports.

Compose implementation requirements:

- Add a `dev-web` target to `dev/Dockerfile`, reuse the existing Bun toolchain,
  mount the workspace, and run the Start dev server on `0.0.0.0:3000`.
- Use named volumes for web dependencies and Bun's cache so the container does
  not replace host `node_modules` or redownload everything on every start.
- Pin the gateway and Electric images to reviewed versions and digests.
- Configure Postgres with `wal_level=logical` and bounded replication slot/WAL
  sender settings required by the pinned Electric release.
- Give Electric a dedicated least-privilege database role with replication plus
  `SELECT` only on explicitly approved sync tables/projections.
- Keep Electric unexposed to the host; all browser shape traffic passes through
  the Go sync endpoints so the client never receives database credentials or an
  Electric secret.
- Set `ADENOSINE_ELECTRIC_URL=http://electric:3000` inside the Go service and
  keep any proxy secret in `.env.local` generated by `scripts/ensure-dev-env.sh`.
- Let `electric` depend on healthy Postgres, but do not make Adenosine readiness
  depend on Electric. The REST-only UI must boot and operate when Electric is
  stopped.
- Make `web` wait for a healthy Adenosine service for predictable development
  startup; make the gateway wait for both HTTP upstreams.
- Add service health checks using endpoints documented by the exact pinned
  versions. Do not guess an Electric health path before pinning the image.
- Update `scripts/dev.sh --detach` and `make doctor` to report/check the public UI
  URL, server-rendered page, API readiness, and Electric availability separately.
- Keep migrations and other Adenosine startup preparation in
  `dev/entrypoint.sh`; do not introduce a one-shot bootstrap service.

The SSR server uses an internal API base URL for network efficiency, but builds
public links from the forwarded host/proto or `ADENOSINE_BASE_URL`. It forwards
only the current request's cookie and required headers. Browser SDK calls stay
relative and same-origin.

The federation-test profile can remain API-only initially. Add the web stack to
that profile only when browser-level federation acceptance tests need it.

## 10. Realtime mutations and reconciliation

All writes call the generated REST SDK.

### Direct local writes

For a mutation that commits directly to the local PostgreSQL transaction, return
the matching PostgreSQL transaction ID and let the Electric collection await that
txid. The txid must be read inside the same transaction as the mutation.

### AT Protocol writes

Stars, issues, comments, statuses, and pull-request records are published to AT
Protocol and may be indexed into PostgreSQL later. A local PostgreSQL txid cannot
identify that future projector transaction.

For these writes:

1. create an optimistic item or state transition;
2. call the REST mutation;
3. capture the returned AT URI and CID;
4. reconcile when Electric emits that URI/CID;
5. show a `publishing` state until it arrives;
6. change to `sync delayed` after a bounded wait instead of claiming the write
   failed when the ATProto publication already succeeded;
7. roll back only when the authoritative REST mutation fails.

Use a shared pending-publication collection and `createOptimisticAction` for this
flow. Do not rely on arbitrary timeouts inside normal collection CRUD handlers.

## 11. Fully typed SDK generation

The repository already generates `packages/api-client` from
`api/openapi.yaml`. Extend that pipeline rather than generating frontend types
directly from PostgreSQL.

Keep the SDK as a separate Bun workspace package. The repository already has the
right workspace roots and a single root `bun.lock`:

```json
{
  "name": "adenosine",
  "private": true,
  "workspaces": ["packages/*", "web"]
}
```

The package boundary is:

```text
packages/api-client/       @adenosine/api-client; generated SDK package
web/                       @adenosine/web; TanStack Start application
package.json               private Bun workspace root
bun.lock                   one lockfile for both packages
```

Declare the SDK as a real dependency of the UI, not a TypeScript path alias:

```json
{
  "name": "@adenosine/web",
  "dependencies": {
    "@adenosine/api-client": "workspace:*"
  }
}
```

`bun install` links the local workspace package, and both browser and SSR builds
import it using normal package imports. Keep request-specific base URLs, cookies,
and headers in UI-created client instances; the generated package must not bake
deployment configuration into its output.

OpenAPI should remain the public source of truth because it describes what every
client may use. PostgreSQL migrations describe storage and may contain private
columns that must never become part of a browser SDK.

Generate these artifacts:

```text
TypeScript request/response/resource types
fetch-based SDK functions
TanStack Query query keys and queryOptions
TanStack Query mutation options
Zod v4 schemas for reusable definitions and request validation
Zod schemas for Sync* collection rows
```

Target configuration shape:

```ts
export default {
  input: './api/openapi.yaml',
  output: './packages/api-client/src/generated',
  plugins: [
    '@hey-api/client-fetch',
    {
      name: 'zod',
      definitions: true,
      requests: true,
      dates: { offset: true },
    },
    {
      name: '@hey-api/sdk',
      validator: true,
    },
    {
      name: '@tanstack/react-query',
      queryOptions: true,
      mutationOptions: true,
      queryKeys: { tags: true },
    },
  ],
}
```

Validate the exact option names against the pinned `@hey-api/openapi-ts` version
when implementing; the repository currently pins `0.99.0`.

Package exports should separate stable surfaces:

```text
@adenosine/api-client             SDK and wire types
@adenosine/api-client/query       generated Query factories
@adenosine/api-client/schemas     generated runtime schemas
```

Represent those surfaces with explicit `exports` in
`packages/api-client/package.json`. Keep the package `private: true` while it is
only used by the monorepo; producing a publishable `dist/` package can be a later
step without changing UI imports.

`make generate` remains the canonical generator entry point. It regenerates the
Go server contract and the `@adenosine/api-client` source together, while CI
fails if generation leaves an uncommitted diff.

SDK client lifetime rules:

- Create a new client for every SSR request and forward only that request's
  cookies/headers. Never mutate a process-wide SSR client.
- Use one browser client configured for same-origin requests with credentials.
- Keep wire timestamps as ISO strings across SSR serialization; format them at
  the presentation boundary.
- Require explicit `operationId`, response schemas, and `additionalProperties`
  policy for every contract object.
- CI runs generation and fails on an uncommitted generated diff.

The generated Zod `Sync*` definitions become each Electric collection's
`schema`, giving runtime validation and inferred collection/live-query types from
the same public contract.

## 12. API gaps to close before each UI slice

The current OpenAPI contract is sufficient for a first repository-browser slice,
identity, repository creation, credentials, stars, and basic issue/comment lists.

The following gaps should be closed in the API before the corresponding UI ships:

1. Documented Electric sync endpoints and `Sync*` row schemas.
2. Repository-filtered/on-demand network discovery and search.
3. Dedicated issue detail lookup and a stable presentation identifier.
4. Pull-request and review REST operations; tables and Lexicons exist, but the
   current OpenAPI surface does not expose them.
5. Repository update/delete/settings operations.
6. A current-user repository list or owner filter for the signed-in dashboard.
7. Profile avatar/media resolution rules.
8. Activity and notification projections.
9. Mutation reconciliation metadata, preferably AT URI/CID for federated writes
   and txid for direct local PostgreSQL writes.

Do not work around missing public APIs with hidden Start server functions.

## 13. Accessibility and responsive behavior

- Meet WCAG 2.2 AA contrast and focus requirements.
- All navigation, clone controls, branch selection, menus, issue actions, and
  diff controls must be keyboard accessible.
- Do not encode local/remote, added/deleted, or pending/confirmed state by color
  alone.
- Code tables expose line numbers and content in a screen-reader-usable order.
- Respect `prefers-reduced-motion` and avoid animating large code surfaces.
- Collapse the context rail below repository content on narrow screens.
- Preserve horizontal scrolling for code; do not shrink code to illegibility.
- Validate dark theme syntax and diff colors independently from the light theme.

## 14. Verification strategy

```text
unit          query factories, schema adapters, optimistic reconciliation
component     repository header, tree, blob, issue thread, credential forms
router        params/search validation, not-found, auth redirects, SSR policy
contract      generated SDK compiles against representative calls
SSR           anonymous landing markup, authenticated redirect, no personal HTML
integration   Go API + Postgres + Electric + web app in dev Compose
end-to-end    login, create repo, browse, star, issue, comment, credential flows
visual        repository overview, code, issue, explore, settings, both themes
accessibility automated checks plus keyboard/manual review
failure       Electric offline, delayed projection, expired session, remote host
```

The rendering/live-query tests must assert all of the following:

1. An unauthenticated `/` response contains the public landing content without
   running browser JavaScript.
2. An authenticated `/` request redirects to `/home` without rendering public or
   personalized page content into that response.
3. `/home` mounts client-side without hydration or missing-`getServerSnapshot`
   warnings.
4. A PostgreSQL projection change reaches the personal homepage live query
   through the documented sync endpoint.
5. Stopping only the Electric service leaves `/home`, navigation, and REST
   mutations working from the Query snapshot/fallback path.
6. Restarting Electric resumes live updates without a full-page reload.
7. `@adenosine/web` type-checks while importing the SDK, Query factories, and
   schemas from the `@adenosine/api-client` workspace package.

## 15. Delivery phases

### Phase 0 — contract and framework spike

- Scaffold TanStack Start under `web/` using Bun.
- Configure Tailwind CSS v4 with `@tailwindcss/vite`.
- Initialize shadcn/ui for TanStack Start and commit `components.json` plus the
  first small component set.
- Add the generated Query and Zod plugins to `packages/api-client`.
- Add `@adenosine/api-client: workspace:*` to `web/package.json` and import a
  representative SDK operation, Query option, and schema from its public exports.
- Prove request-scoped SSR client cookie forwarding for the `/` identity check.
- Prove anonymous landing SSR and authenticated redirect to client-only `/home`.
- Add one safe Electric sync endpoint and one typed collection.
- Prove the personal homepage's client-side REST snapshot, live query, and
  REST-only fallback.
- Add `web`, `electric`, and the same-origin gateway to
  `dev/docker-compose.yml`, including logical-replication configuration and
  health checks.

Exit condition: the anonymous landing page is useful with JavaScript disabled,
authenticated `/` requests redirect to `/home`, and the personal homepage becomes
live after its client render while still working with Electric stopped. The UI
imports the generated SDK only through its workspace package, the browser console
has no hydration or `getServerSnapshot` errors, and the whole spike starts through
`make dev` without host-installed Bun or Node.

### Phase 1 — shell and repository browser

- Design tokens, global shell, theme, responsive navigation, error boundaries.
- Explore page and repository overview.
- Branch/tag selector, tree, blob/README, commits, commit detail, diff.
- Clone control and local/remote hosting treatment.

This phase uses the API that exists today and provides the first coherent product
experience.

### Phase 2 — identity and self-hosting essentials

- ATProto login and passkey login.
- Create repository.
- Profile editing.
- Passkeys, SSH keys, access tokens, and moderation settings.

### Phase 3 — live collaboration

- Typed repository/profile/issue/comment/star collections.
- Issue list/detail and comment composer.
- Star optimistic action.
- Pending-publication reconciliation and delayed-index UX.
- Live update status and REST fallback behavior.

### Phase 4 — pull requests

- First add the missing public PR/review API contract.
- PR list/detail, compare view, reviews, status, and merge.
- Live review/status projections and Git diff reads through Query.

### Phase 5 — network depth and polish

- On-demand network search and discovery.
- Activity/notifications.
- Performance budgets, deferred hydration where it materially helps, visual
  regression coverage, and third-party SDK publishing.

## 16. Decisions to keep stable

```text
REST owns writes.
Electric owns optional realtime projection delivery.
Query owns browser REST and Git request/response state.
TanStack DB owns browser-side normalized/live projection queries.
Tailwind CSS and shadcn/ui own the initial presentation foundation.
OpenAPI owns the public SDK contract.
The official UI has no privileged data path.
Only the anonymous `/` landing page is SSR.
Authenticated `/` requests redirect to the client-rendered `/home` route.
All product routes, including public repository/profile pages, use `ssr: false`.
Whole-network eager sync is prohibited.
ATProto publication success is not confused with local projection completion.
Released TanStack DB live-query hooks never execute during SSR.
The default development UI/Electric stack lives in one Compose file.
Browser HTTP traffic is same-origin through the development gateway.
The generated SDK is a separate @adenosine/api-client Bun workspace dependency.
```

## 17. Current upstream references

- [TanStack Start overview](https://tanstack.com/start/latest/docs/framework/react/overview)
- [TanStack Start execution model: ClientOnly and useHydrated](https://tanstack.com/start/latest/docs/framework/react/guide/execution-model)
- [TanStack DB overview](https://tanstack.com/db/latest/docs/overview)
- [TanStack DB live queries](https://tanstack.com/db/latest/docs/guides/live-queries)
- [TanStack DB Electric collection](https://tanstack.com/db/latest/docs/collections/electric-collection)
- [Open TanStack DB SSR issue: missing getServerSnapshot](https://github.com/TanStack/db/issues/1016)
- [Draft TanStack DB SSR replacement](https://github.com/TanStack/db/pull/1564)
- [Electric Postgres sync deployment](https://electric.ax/sync/postgres-sync)
- [Electric authentication and proxy guidance](https://electric.ax/docs/sync/guides/auth)
- [Electric security guidance](https://electric.ax/docs/sync/guides/security)
- [shadcn/ui for TanStack Start](https://ui.shadcn.com/docs/installation/tanstack)
- [Tailwind CSS with Vite](https://tailwindcss.com/docs/installation/using-vite)
- [Hey API TanStack Query plugin](https://heyapi.dev/docs/openapi/typescript/plugins/tanstack-query)
- [Hey API Zod plugin](https://heyapi.dev/docs/openapi/typescript/plugins/zod)
- [Bun workspaces](https://bun.sh/docs/pm/workspaces)

TanStack Start is currently documented as release-candidate software and the
TanStack DB/Electric integration is evolving. Pin exact versions, regenerate the
lockfile deliberately, and validate the Phase 0 spike before expanding the UI.
