CREATE INDEX network_repositories_discovery_idx
    ON network.repositories (indexed_at DESC, uri DESC)
    WHERE deleted_at IS NULL;
