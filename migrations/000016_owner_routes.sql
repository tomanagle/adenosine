CREATE TABLE core.owner_routes (
    alias TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    account_did TEXT REFERENCES core.accounts(did) ON DELETE CASCADE,
    organization_id UUID REFERENCES core.organizations(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT owner_routes_alias_format CHECK (
        alias ~ '^[a-z0-9][a-z0-9.-]*$' AND length(alias) <= 253
    ),
    CONSTRAINT owner_routes_kind CHECK (kind IN ('account', 'organization')),
    CONSTRAINT owner_routes_target CHECK (
        (kind = 'account' AND account_did IS NOT NULL AND organization_id IS NULL)
        OR (kind = 'organization' AND organization_id IS NOT NULL AND account_did IS NULL)
    ),
    CONSTRAINT owner_routes_reserved_alias CHECK (
        lower(alias) NOT IN (
            'api', 'docs', 'explore', 'health', 'login', 'oauth',
            'organizations', 'profiles', 'settings'
        )
    )
);

CREATE UNIQUE INDEX owner_routes_alias_uidx ON core.owner_routes (lower(alias));
CREATE INDEX owner_routes_account_idx ON core.owner_routes (account_did) WHERE account_did IS NOT NULL;
CREATE INDEX owner_routes_organization_idx ON core.owner_routes (organization_id) WHERE organization_id IS NOT NULL;

INSERT INTO core.owner_routes (alias, kind, account_did, created_at)
SELECT lower(handle_cache), 'account', did, created_at
FROM (
    SELECT DISTINCT ON (lower(handle_cache)) handle_cache, did, created_at
    FROM core.accounts
    WHERE handle_cache IS NOT NULL AND handle_cache <> ''
    ORDER BY lower(handle_cache), last_seen_at DESC, did
) AS current_handle;

INSERT INTO core.owner_routes (alias, kind, organization_id, created_at)
SELECT lower(slug), 'organization', id, created_at
FROM core.organizations
WHERE deleted_at IS NULL;
