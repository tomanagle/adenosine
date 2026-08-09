# API Authentication

Adenosine REST endpoints authenticate either an existing browser session or a personal access token, as declared per operation in OpenAPI.

Browser sessions use the `adenosine_session` cookie. Only a SHA-256 hash of the high-entropy session credential is stored. Active-session lookup rejects expired and revoked sessions and updates `last_seen_at` atomically. Session issuance will be owned by the ATProto OAuth flow; no username/password or unauthenticated session-creation endpoint exists.

Personal access tokens use:

```http
Authorization: Bearer adn_pat_...
```

Only the token hash and a safe display prefix are persisted. The complete plaintext is returned once by `POST /api/v1/tokens`.

Credential administration is deliberately session-only:

```text
GET    /api/v1/tokens
POST   /api/v1/tokens
DELETE /api/v1/tokens/{id}
GET    /api/v1/ssh-keys
POST   /api/v1/ssh-keys
DELETE /api/v1/ssh-keys/{id}
```

This prevents a repository-scoped PAT from creating stronger account credentials. Cookie-authenticated mutations must include an `Origin` header exactly matching `ADENOSINE_BASE_URL`; this is the CSRF boundary in addition to future cookie attributes established by OAuth session issuance.

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
