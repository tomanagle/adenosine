CREATE TABLE core.releases (
    id UUID PRIMARY KEY,
    repository_id UUID NOT NULL REFERENCES core.repositories(id) ON DELETE CASCADE,
    tag_name TEXT NOT NULL,
    target_sha TEXT NOT NULL,
    name TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'draft',
    prerelease BOOLEAN NOT NULL DEFAULT FALSE,
    created_by_did TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ,
    CONSTRAINT releases_tag_name_length CHECK (length(tag_name) BETWEEN 1 AND 1024),
    CONSTRAINT releases_target_sha_format CHECK (target_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    CONSTRAINT releases_name_length CHECK (length(name) BETWEEN 1 AND 255),
    CONSTRAINT releases_body_length CHECK (length(body) <= 1048576),
    CONSTRAINT releases_state_valid CHECK (state IN ('draft', 'published', 'deleting')),
    CONSTRAINT releases_creator_nonempty CHECK (length(created_by_did) BETWEEN 1 AND 2048),
    CONSTRAINT releases_published_at_consistent CHECK (
        (state = 'draft' AND published_at IS NULL)
        OR (state = 'published' AND published_at IS NOT NULL)
        OR state = 'deleting'
    ),
    CONSTRAINT releases_id_repository_unique UNIQUE (id, repository_id),
    UNIQUE (repository_id, tag_name)
);

CREATE INDEX releases_repository_page_idx
    ON core.releases (repository_id, created_at DESC, id DESC);

CREATE INDEX releases_repository_published_page_idx
    ON core.releases (repository_id, created_at DESC, id DESC)
    WHERE state = 'published';

CREATE TABLE core.release_assets (
    id UUID PRIMARY KEY,
    release_id UUID NOT NULL,
    repository_id UUID NOT NULL,
    name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    sha256 TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT release_assets_name_length CHECK (length(name) BETWEEN 1 AND 255),
    CONSTRAINT release_assets_name_safe CHECK (name NOT IN ('.', '..') AND name !~ '[\\/]'),
    CONSTRAINT release_assets_content_type_length CHECK (length(content_type) BETWEEN 1 AND 255),
    CONSTRAINT release_assets_size_nonnegative CHECK (size_bytes >= 0),
    CONSTRAINT release_assets_sha256_format CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT release_assets_storage_key_safe CHECK (
        length(storage_key) BETWEEN 1 AND 1024
        AND storage_key !~ '(^/|(^|/)\.\.(/|$))'
    ),
    CONSTRAINT release_assets_release_repository_fk
        FOREIGN KEY (release_id, repository_id)
        REFERENCES core.releases(id, repository_id)
        ON DELETE CASCADE,
    UNIQUE (release_id, name),
    UNIQUE (storage_key)
);

CREATE INDEX release_assets_release_page_idx
    ON core.release_assets (release_id, created_at DESC, id DESC);

CREATE INDEX release_assets_repository_usage_idx
    ON core.release_assets (repository_id);
