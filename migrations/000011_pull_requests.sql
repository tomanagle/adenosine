ALTER TABLE network.repositories
    ADD COLUMN pull_request_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN open_pull_request_count BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT network_repositories_pull_request_count_nonnegative CHECK (pull_request_count >= 0),
    ADD CONSTRAINT network_repositories_open_pull_request_count_nonnegative CHECK (open_pull_request_count >= 0);

CREATE INDEX network_repositories_open_pull_request_count_idx
    ON network.repositories (open_pull_request_count DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE network.pull_requests (
    uri                    TEXT PRIMARY KEY,
    cid                    TEXT,
    author_did             TEXT NOT NULL,
    rkey                   TEXT NOT NULL,
    source_repository_uri  TEXT NOT NULL,
    source_repository_cid  TEXT NOT NULL,
    source_branch          TEXT NOT NULL,
    target_repository_uri  TEXT NOT NULL,
    target_repository_cid  TEXT NOT NULL,
    target_branch          TEXT NOT NULL,
    head_sha               TEXT NOT NULL,
    title                  TEXT NOT NULL,
    body                   TEXT NOT NULL,
    record_created_at      TIMESTAMPTZ NOT NULL,
    record_updated_at      TIMESTAMPTZ NOT NULL,
    indexed_at             TIMESTAMPTZ NOT NULL,
    deleted_at             TIMESTAMPTZ,
    source_event_id        BIGINT NOT NULL,
    state                  TEXT NOT NULL DEFAULT 'open',
    status_uri             TEXT,
    status_cid             TEXT,
    status_updated_at      TIMESTAMPTZ,
    status_source_event_id BIGINT,
    merged_commit_sha      TEXT,
    review_count           BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT network_pull_requests_event_id_positive CHECK (source_event_id > 0),
    CONSTRAINT network_pull_requests_status_event_id_positive CHECK (
        status_source_event_id IS NULL OR status_source_event_id > 0
    ),
    CONSTRAINT network_pull_requests_state CHECK (state IN ('open', 'closed', 'merged')),
    CONSTRAINT network_pull_requests_live_value CHECK (deleted_at IS NOT NULL OR cid IS NOT NULL),
    CONSTRAINT network_pull_requests_review_count_nonnegative CHECK (review_count >= 0),
    CONSTRAINT network_pull_requests_status_shape CHECK (
        (status_uri IS NULL AND status_cid IS NULL AND status_updated_at IS NULL AND status_source_event_id IS NULL AND merged_commit_sha IS NULL)
        OR
        (status_uri IS NOT NULL AND status_cid IS NOT NULL AND status_updated_at IS NOT NULL AND status_source_event_id IS NOT NULL)
    ),
    CONSTRAINT network_pull_requests_merged_shape CHECK (
        (state = 'merged' AND merged_commit_sha IS NOT NULL)
        OR (state <> 'merged' AND merged_commit_sha IS NULL)
    )
);

CREATE INDEX network_pull_requests_target_state_idx
    ON network.pull_requests (target_repository_uri, state, record_created_at DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX network_pull_requests_source_idx
    ON network.pull_requests (source_repository_uri, record_created_at DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX network_pull_requests_author_idx
    ON network.pull_requests (author_did, record_created_at DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE network.pull_request_statuses (
    uri                   TEXT PRIMARY KEY,
    cid                   TEXT,
    author_did            TEXT NOT NULL,
    rkey                  TEXT NOT NULL,
    pull_request_uri      TEXT NOT NULL,
    pull_request_cid      TEXT NOT NULL,
    target_repository_uri TEXT NOT NULL,
    target_repository_cid TEXT NOT NULL,
    state                 TEXT NOT NULL,
    merged_commit_sha     TEXT,
    record_created_at     TIMESTAMPTZ NOT NULL,
    record_updated_at     TIMESTAMPTZ NOT NULL,
    indexed_at            TIMESTAMPTZ NOT NULL,
    deleted_at            TIMESTAMPTZ,
    source_event_id       BIGINT NOT NULL,
    CONSTRAINT network_pull_request_statuses_event_id_positive CHECK (source_event_id > 0),
    CONSTRAINT network_pull_request_statuses_state CHECK (state IN ('open', 'closed', 'merged')),
    CONSTRAINT network_pull_request_statuses_live_value CHECK (deleted_at IS NOT NULL OR cid IS NOT NULL),
    CONSTRAINT network_pull_request_statuses_merged_shape CHECK (
        (state = 'merged' AND merged_commit_sha IS NOT NULL)
        OR (state <> 'merged' AND merged_commit_sha IS NULL)
    )
);

CREATE INDEX network_pull_request_statuses_subject_idx
    ON network.pull_request_statuses (pull_request_uri, source_event_id DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX network_pull_request_statuses_target_idx
    ON network.pull_request_statuses (target_repository_uri, source_event_id DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE network.pull_request_reviews (
    uri                TEXT PRIMARY KEY,
    cid                TEXT,
    author_did         TEXT NOT NULL,
    rkey               TEXT NOT NULL,
    pull_request_uri   TEXT NOT NULL,
    pull_request_cid   TEXT NOT NULL,
    body               TEXT NOT NULL,
    verdict            TEXT NOT NULL,
    record_created_at  TIMESTAMPTZ NOT NULL,
    record_updated_at  TIMESTAMPTZ NOT NULL,
    indexed_at         TIMESTAMPTZ NOT NULL,
    deleted_at         TIMESTAMPTZ,
    source_event_id    BIGINT NOT NULL,
    CONSTRAINT network_pull_request_reviews_event_id_positive CHECK (source_event_id > 0),
    CONSTRAINT network_pull_request_reviews_verdict CHECK (verdict IN ('comment', 'approve', 'request_changes')),
    CONSTRAINT network_pull_request_reviews_live_value CHECK (deleted_at IS NOT NULL OR cid IS NOT NULL)
);

CREATE INDEX network_pull_request_reviews_subject_idx
    ON network.pull_request_reviews (pull_request_uri, record_created_at, uri)
    WHERE deleted_at IS NULL;

CREATE INDEX network_pull_request_reviews_author_idx
    ON network.pull_request_reviews (author_did, record_created_at DESC, uri DESC)
    WHERE deleted_at IS NULL;
