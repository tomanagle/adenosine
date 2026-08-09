ALTER TABLE network.repositories
    ADD COLUMN star_count BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT network_repositories_star_count_nonnegative CHECK (star_count >= 0);

CREATE INDEX network_repositories_star_count_idx
    ON network.repositories (star_count DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE network.stars (
    uri                 TEXT PRIMARY KEY,
    cid                 TEXT,
    author_did          TEXT NOT NULL,
    rkey                TEXT NOT NULL,
    repository_uri      TEXT NOT NULL,
    repository_cid      TEXT NOT NULL,
    record_created_at   TIMESTAMPTZ NOT NULL,
    indexed_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ,
    source_event_id     BIGINT NOT NULL,
    CONSTRAINT network_stars_event_id_positive CHECK (source_event_id > 0),
    CONSTRAINT network_stars_live_value CHECK (
        deleted_at IS NOT NULL OR cid IS NOT NULL
    )
);

CREATE UNIQUE INDEX network_stars_author_repository_active_uidx
    ON network.stars (author_did, repository_uri)
    WHERE deleted_at IS NULL;

CREATE INDEX network_stars_repository_idx
    ON network.stars (repository_uri, indexed_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX network_stars_author_idx
    ON network.stars (author_did, indexed_at DESC)
    WHERE deleted_at IS NULL;
