# REST API

## Contract and versioning

[`../api/openapi.yaml`](../api/openapi.yaml) is the installed public contract. Its current
release is `0.1.0`, OpenAPI `3.0.3`. The running instance serves documentation at
`/docs/api` and the installed JSON contract at `/openapi.json`. Instance documentation and
generated clients must be produced from that exact file, not a newer contract from main.

Public application endpoints are under `/api/v1`; incompatible wire changes require a new
major path. Additive fields and operations may be introduced within v1. Every operation
must have an `operationId`, explicit request/response schemas, and declared authentication.
REST remains the canonical write path and complete fallback when realtime is unavailable.

## Requests and responses

Use the OpenAPI operation rather than guessing behavior. The current conventions are:

- Browser sessions or bearer PATs authenticate only where declared. See [authentication](api-authentication.md).
- JSON request bodies reject malformed input; raw Git blob responses are byte streams.
- List envelopes contain `data` and `page.next_cursor`. Network repository discovery uses
  opaque cursor pagination with a bounded `limit`; do not inspect or construct cursors.
- `GET /api/v1/search/repositories` and `GET /api/v1/search/profiles` are the canonical
  search reads. Their opaque cursors are bound to the operation, query, and sort. They read
  only the local AppView, accept anonymous requests, and apply account-local moderation for
  a valid browser session. An invalid presented session returns `401` rather than becoming
  anonymous.
- Filters are operation-specific query parameters. Sync subset predicates may only narrow
  a server-owned shape; they cannot broaden visibility.
- Errors use `{"error":{"code":"...","message":"...","request_id":"..."}}` and the
  same identifier appears in `X-Request-ID`.
- `409` represents a state/ref/CID conflict; validation uses `400` or `422` as declared;
  bounded Git output may return `413`; unavailable upstreams use declared `502`/`503` errors.

`Idempotency-Key` is currently declared only on repository creation but is reserved: the
handler does not yet persist or replay keys. Clients must not assume retry deduplication.
Federation ingestion itself is replay-safe by event identity, and PR merge uses exact refs,
controlled fetch refs, and merge recognition, but those safeguards do not create a general
REST idempotency guarantee.

## Example

```sh
curl -fsS \
  -H "Authorization: Bearer $ADENOSINE_TOKEN" \
  http://127.0.0.1:8080/api/v1/me
```

Generated TypeScript client from this checkout:

```ts
import { createClient, getCurrentIdentity } from '@adenosine/api-client'

const client = createClient({
  baseUrl: 'https://code.example',
  headers: { Authorization: `Bearer ${token}` },
})
const result = await getCurrentIdentity({ client })
```

The package is currently a private Bun workspace package, not a published npm artifact.
External clients can generate from an instance's `/openapi.json` or call REST directly.
Federated records are eventually consistent: a successful publishing response can precede
its appearance in list, projection, or sync results. Use returned AT URI/CID values to
reconcile; retain REST reads and normal refetching when Electric is delayed or unavailable.
Search has the same projection lag: it never fans out to a remote forge or fetches a remote
profile at request time. Repository results include indexed canonical web and Git
destinations, and ranking has no local-host preference.

## Generated ownership

Inputs are `api/openapi.yaml`, `api/oapi-codegen.yaml`, and
`packages/api-client/openapi-ts.config.ts`. `make generate` writes
`api/generated/go/` and `packages/api-client/src/generated/`. Do not hand-edit those
directories. The small package export files outside `src/generated/` are maintained by
hand. Run generation and commit input and output changes together.
