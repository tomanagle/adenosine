# Public API

[`openapi.yaml`](openapi.yaml) is the canonical installed Adenosine REST contract. This
checkout serves API release `0.1.0` (OpenAPI 3.0.3) at `/openapi.json` and interactive
documentation at `/docs/api`. Instance clients should generate against the instance's
installed document rather than assuming main has the same release.

```sh
curl -fsS -H "Authorization: Bearer $ADENOSINE_TOKEN" \
  https://code.example/api/v1/me
```

```ts
import { createClient, getCurrentIdentity } from '@adenosine/api-client'

const client = createClient({ baseUrl: 'https://code.example' })
const identity = await getCurrentIdentity({ client })
```

The TypeScript package is currently an internal Bun workspace package. Third-party clients
can generate from `/openapi.json` or use curl/HTTP directly. REST owns all writes and is the
complete fallback when Electric is absent. ATProto publication and network reads are
eventually consistent, so reconcile writes by returned AT URI/CID and do not assume they
appear immediately in projections.

Run `make generate` after changing the contract. `api/generated/go/` and
`packages/api-client/src/generated/` are outputs; do not hand-edit them. See
[`../docs/api.md`](../docs/api.md) for authentication, pagination, errors, idempotency,
versioning, and generated ownership.
