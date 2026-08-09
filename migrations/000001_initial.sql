CREATE SCHEMA auth;
CREATE SCHEMA core;
CREATE SCHEMA network;
CREATE SCHEMA ops;

CREATE TABLE core.accounts (
    did TEXT PRIMARY KEY,
    handle_cache TEXT,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT accounts_did_nonempty CHECK (length(did) > 0)
);

CREATE INDEX accounts_handle_cache_idx ON core.accounts (lower(handle_cache));

CREATE TABLE auth.sessions (
    id UUID PRIMARY KEY,
    account_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    user_agent_hash BYTEA,
    ip_prefix TEXT
);

CREATE INDEX sessions_account_active_idx
    ON auth.sessions (account_did, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE auth.ssh_keys (
    id UUID PRIMARY KEY,
    account_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE CASCADE,
    name TEXT NOT NULL,
    algorithm TEXT NOT NULL,
    public_key TEXT NOT NULL,
    fingerprint TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX ssh_keys_account_idx
    ON auth.ssh_keys (account_did)
    WHERE revoked_at IS NULL;

CREATE TABLE core.repositories (
    id UUID PRIMARY KEY,
    owner_did TEXT NOT NULL REFERENCES core.accounts(did),
    slug TEXT NOT NULL,
    display_name TEXT,
    description TEXT,
    visibility TEXT NOT NULL DEFAULT 'public',
    state TEXT NOT NULL DEFAULT 'creating',
    default_branch TEXT NOT NULL DEFAULT 'main',
    storage_key TEXT NOT NULL UNIQUE,
    at_uri TEXT UNIQUE,
    at_cid TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ,
    CONSTRAINT repositories_slug_format CHECK (slug ~ '^[a-z0-9][a-z0-9._-]*$'),
    CONSTRAINT repositories_visibility CHECK (visibility IN ('public', 'private')),
    CONSTRAINT repositories_state CHECK (state IN ('creating', 'active', 'failed', 'deleting', 'deleted'))
);

CREATE UNIQUE INDEX repositories_owner_slug_active_uidx
    ON core.repositories (owner_did, lower(slug))
    WHERE deleted_at IS NULL;

CREATE INDEX repositories_owner_idx
    ON core.repositories (owner_did)
    WHERE deleted_at IS NULL;

CREATE INDEX repositories_at_uri_idx
    ON core.repositories (at_uri)
    WHERE at_uri IS NOT NULL;

CREATE TABLE auth.access_tokens (
    id UUID PRIMARY KEY,
    account_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE,
    scopes TEXT[] NOT NULL,
    repository_id UUID REFERENCES core.repositories(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX access_tokens_account_idx
    ON auth.access_tokens (account_did)
    WHERE revoked_at IS NULL;

CREATE TABLE core.repository_collaborators (
    repository_id UUID NOT NULL REFERENCES core.repositories(id) ON DELETE CASCADE,
    account_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE CASCADE,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repository_id, account_did),
    CONSTRAINT repository_collaborator_role CHECK (role IN ('read', 'write', 'maintain', 'admin'))
);

CREATE INDEX repository_collaborators_account_idx
    ON core.repository_collaborators (account_did);

CREATE TABLE core.repository_aliases (
    id UUID PRIMARY KEY,
    repository_id UUID NOT NULL REFERENCES core.repositories(id) ON DELETE CASCADE,
    owner_alias TEXT NOT NULL,
    slug_alias TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX repository_alias_lookup_uidx
    ON core.repository_aliases (lower(owner_alias), lower(slug_alias));

CREATE TABLE ops.outbox_events (
    id UUID PRIMARY KEY,
    type TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ NOT NULL,
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    completed_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error_code TEXT,
    CONSTRAINT outbox_attempts_nonnegative CHECK (attempts >= 0)
);

CREATE INDEX outbox_ready_idx
    ON ops.outbox_events (available_at, created_at)
    WHERE completed_at IS NULL;
