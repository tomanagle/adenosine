# API Authentication

Adenosine REST endpoints authenticate either an existing browser session or a personal access token, as declared per operation in OpenAPI.

Browser sessions use the `adenosine_session` cookie. Only a SHA-256 hash of the
high-entropy session credential is stored. Active-session lookup rejects expired and
revoked sessions and updates `last_seen_at` atomically. ATProto OAuth and passkey login
issue local sessions; there is no username/password or unauthenticated session-creation
endpoint.

Personal access tokens use:

```http
Authorization: Bearer adn_pat_...
```

Only the token hash and a safe display prefix are persisted. The complete plaintext is returned once by `POST /api/v1/tokens`.

PAT scopes and optional repository restriction are enforced per operation. A
repository-restricted token cannot administer account credentials, and credential
administration remains session-only even if a PAT has repository write scope.
Issue creation, pull request creation, and pull request merge accept an account-wide
`repository:write` PAT. They reject repository-restricted PATs because their portable
AT URIs may identify repositories on another instance and cannot be safely matched to one
local repository UUID. Cookie-authenticated forms continue to require exact-origin CSRF
validation; PAT requests do not send an `Origin` header.

Credential administration is deliberately session-only:

```text
GET    /api/v1/tokens
POST   /api/v1/tokens
DELETE /api/v1/tokens/{id}
GET    /api/v1/ssh-keys
POST   /api/v1/ssh-keys
DELETE /api/v1/ssh-keys/{id}
```

This prevents a repository-scoped PAT from creating stronger account credentials.
Cookie-authenticated mutations must include an `Origin` header exactly matching
`ADENOSINE_BASE_URL`; this is the CSRF boundary alongside the session cookie attributes.

List responses never contain token hashes or plaintext secrets. Delete operations soft-revoke credentials and atomically scope the update to the authenticated DID, so another account's credential ID is indistinguishable from a missing ID.

REST errors use a stable JSON envelope and return the same request identifier in `X-Request-ID` and `error.request_id`:

```json
{
  "error": {
    "code": "authentication_required",
    "message": "Authentication is required",
    "request_id": "..."
  }
}
```
