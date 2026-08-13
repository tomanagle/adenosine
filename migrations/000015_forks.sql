ALTER TABLE core.repositories
    ADD COLUMN forked_from_uri TEXT,
    ADD COLUMN forked_from_cid TEXT,
    ADD COLUMN forked_from_local_repository_id UUID REFERENCES core.repositories(id) ON DELETE SET NULL,
    ADD COLUMN fork_count BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT repositories_fork_strong_ref_complete CHECK (
        (forked_from_uri IS NULL) = (forked_from_cid IS NULL)
    ),
    ADD CONSTRAINT repositories_fork_not_self CHECK (
        forked_from_local_repository_id IS NULL OR forked_from_local_repository_id <> id
    ),
    ADD CONSTRAINT repositories_fork_count_nonnegative CHECK (
        fork_count >= 0
    );

CREATE INDEX repositories_fork_source_idx
    ON core.repositories (forked_from_uri, created_at DESC, id DESC)
    WHERE forked_from_uri IS NOT NULL AND deleted_at IS NULL;

ALTER TABLE network.repositories
    ADD COLUMN forked_from_uri TEXT,
    ADD COLUMN forked_from_cid TEXT,
    ADD COLUMN fork_count BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT network_repositories_fork_strong_ref_complete CHECK (
        (forked_from_uri IS NULL) = (forked_from_cid IS NULL)
    ),
    ADD CONSTRAINT network_repositories_fork_count_nonnegative CHECK (fork_count >= 0);

CREATE INDEX network_repositories_fork_source_idx
    ON network.repositories (forked_from_uri, indexed_at DESC, uri DESC)
    WHERE forked_from_uri IS NOT NULL AND deleted_at IS NULL;
