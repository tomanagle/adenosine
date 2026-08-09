CREATE SCHEMA moderation;

ALTER TABLE network.issues
    ADD COLUMN comment_count BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT network_issues_comment_count_nonnegative CHECK (comment_count >= 0);

ALTER TABLE network.repositories
    ADD COLUMN comment_count BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT network_repositories_comment_count_nonnegative CHECK (comment_count >= 0);

CREATE TABLE network.issue_comments (
    uri                 TEXT PRIMARY KEY,
    cid                 TEXT,
    author_did          TEXT NOT NULL,
    rkey                TEXT NOT NULL,
    issue_uri           TEXT NOT NULL,
    issue_cid           TEXT NOT NULL,
    parent_uri          TEXT,
    parent_cid          TEXT,
    body                TEXT NOT NULL,
    record_created_at   TIMESTAMPTZ NOT NULL,
    record_updated_at   TIMESTAMPTZ NOT NULL,
    indexed_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ,
    source_event_id     BIGINT NOT NULL,
    CONSTRAINT network_issue_comments_event_id_positive CHECK (source_event_id > 0),
    CONSTRAINT network_issue_comments_live_value CHECK (deleted_at IS NOT NULL OR cid IS NOT NULL),
    CONSTRAINT network_issue_comments_parent_shape CHECK (
        (parent_uri IS NULL AND parent_cid IS NULL)
        OR (parent_uri IS NOT NULL AND parent_cid IS NOT NULL)
    )
);

CREATE INDEX network_issue_comments_issue_idx
    ON network.issue_comments (issue_uri, record_created_at, uri)
    WHERE deleted_at IS NULL;

CREATE INDEX network_issue_comments_parent_idx
    ON network.issue_comments (parent_uri, record_created_at, uri)
    WHERE deleted_at IS NULL AND parent_uri IS NOT NULL;

CREATE INDEX network_issue_comments_author_idx
    ON network.issue_comments (author_did, record_created_at DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE moderation.blocked_dids (
    account_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE CASCADE,
    blocked_did TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (account_did, blocked_did)
);

CREATE TABLE moderation.hidden_records (
    account_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE CASCADE,
    record_uri  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (account_did, record_uri)
);
