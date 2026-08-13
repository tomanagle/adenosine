CREATE TABLE network.pull_request_review_requests (
    uri                    TEXT PRIMARY KEY,
    cid                    TEXT,
    author_did             TEXT NOT NULL,
    rkey                   TEXT NOT NULL,
    pull_request_uri       TEXT NOT NULL,
    pull_request_cid       TEXT NOT NULL,
    target_repository_uri  TEXT NOT NULL,
    target_repository_cid  TEXT NOT NULL,
    reviewer_did           TEXT NOT NULL,
    requested_by_did       TEXT NOT NULL,
    record_created_at      TIMESTAMPTZ NOT NULL,
    record_updated_at      TIMESTAMPTZ NOT NULL,
    indexed_at             TIMESTAMPTZ NOT NULL,
    deleted_at             TIMESTAMPTZ,
    source_event_id        BIGINT NOT NULL,
    CONSTRAINT network_pull_request_review_requests_event_id_positive
        CHECK (source_event_id > 0),
    CONSTRAINT network_pull_request_review_requests_live_value
        CHECK (deleted_at IS NOT NULL OR cid IS NOT NULL),
    CONSTRAINT network_pull_request_review_requests_time_order
        CHECK (record_updated_at >= record_created_at)
);

CREATE INDEX network_pull_request_review_requests_subject_idx
    ON network.pull_request_review_requests
       (pull_request_uri, record_updated_at DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX network_pull_request_review_requests_reviewer_idx
    ON network.pull_request_review_requests
       (reviewer_did, record_updated_at DESC, uri DESC)
    WHERE deleted_at IS NULL;
