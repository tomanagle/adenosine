CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX network_repositories_search_text_idx
    ON network.repositories USING GIN (
        to_tsvector('simple', coalesce(name, '') || ' ' || coalesce(slug, '') || ' ' || coalesce(description, ''))
    )
    WHERE deleted_at IS NULL AND cid IS NOT NULL;

CREATE INDEX network_repositories_name_trgm_idx
    ON network.repositories USING GIN (lower(name) gin_trgm_ops)
    WHERE deleted_at IS NULL AND cid IS NOT NULL;

CREATE INDEX network_repositories_slug_trgm_idx
    ON network.repositories USING GIN (lower(slug) gin_trgm_ops)
    WHERE deleted_at IS NULL AND cid IS NOT NULL;

CREATE INDEX network_repositories_description_trgm_idx
    ON network.repositories USING GIN (lower(description) gin_trgm_ops)
    WHERE deleted_at IS NULL AND cid IS NOT NULL;

CREATE INDEX network_profiles_search_text_idx
    ON network.profiles USING GIN (
        to_tsvector('simple', coalesce(handle, '') || ' ' || coalesce(display_name, ''))
    )
    WHERE deleted_at IS NULL;

CREATE INDEX network_profiles_handle_trgm_idx
    ON network.profiles USING GIN (lower(handle) gin_trgm_ops)
    WHERE deleted_at IS NULL;

CREATE INDEX network_profiles_display_name_trgm_idx
    ON network.profiles USING GIN (lower(display_name) gin_trgm_ops)
    WHERE deleted_at IS NULL;

CREATE INDEX network_identities_handle_trgm_idx
    ON network.identities USING GIN (lower(handle) gin_trgm_ops)
    WHERE is_active;
