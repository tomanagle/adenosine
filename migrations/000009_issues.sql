ALTER TABLE network.repositories
    ADD COLUMN issue_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN open_issue_count BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT network_repositories_issue_count_nonnegative CHECK (issue_count >= 0),
    ADD CONSTRAINT network_repositories_open_issue_count_nonnegative CHECK (open_issue_count >= 0);

CREATE INDEX network_repositories_open_issue_count_idx
    ON network.repositories (open_issue_count DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE network.issues (
    uri                    TEXT PRIMARY KEY,
    cid                    TEXT,
    author_did             TEXT NOT NULL,
    rkey                   TEXT NOT NULL,
    repository_uri         TEXT NOT NULL,
    repository_cid         TEXT NOT NULL,
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
    CONSTRAINT network_issues_event_id_positive CHECK (source_event_id > 0),
    CONSTRAINT network_issues_status_event_id_positive CHECK (
        status_source_event_id IS NULL OR status_source_event_id > 0
    ),
    CONSTRAINT network_issues_state CHECK (state IN ('open', 'closed')),
    CONSTRAINT network_issues_live_value CHECK (deleted_at IS NOT NULL OR cid IS NOT NULL),
    CONSTRAINT network_issues_status_shape CHECK (
        (status_uri IS NULL AND status_cid IS NULL AND status_updated_at IS NULL AND status_source_event_id IS NULL)
        OR
        (status_uri IS NOT NULL AND status_cid IS NOT NULL AND status_updated_at IS NOT NULL AND status_source_event_id IS NOT NULL)
    )
);

CREATE INDEX network_issues_repository_state_idx
    ON network.issues (repository_uri, state, record_created_at DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX network_issues_author_idx
    ON network.issues (author_did, record_created_at DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE network.issue_statuses (
    uri                 TEXT PRIMARY KEY,
    cid                 TEXT,
    author_did          TEXT NOT NULL,
    rkey                TEXT NOT NULL,
    issue_uri           TEXT NOT NULL,
    issue_cid           TEXT NOT NULL,
    repository_uri      TEXT NOT NULL,
    repository_cid      TEXT NOT NULL,
    state               TEXT NOT NULL,
    record_created_at   TIMESTAMPTZ NOT NULL,
    record_updated_at   TIMESTAMPTZ NOT NULL,
    indexed_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ,
    source_event_id     BIGINT NOT NULL,
    CONSTRAINT network_issue_statuses_event_id_positive CHECK (source_event_id > 0),
    CONSTRAINT network_issue_statuses_state CHECK (state IN ('open', 'closed')),
    CONSTRAINT network_issue_statuses_live_value CHECK (deleted_at IS NOT NULL OR cid IS NOT NULL)
);

CREATE INDEX network_issue_statuses_issue_idx
    ON network.issue_statuses (issue_uri, source_event_id DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX network_issue_statuses_repository_idx
    ON network.issue_statuses (repository_uri, source_event_id DESC, uri DESC)
    WHERE deleted_at IS NULL;
