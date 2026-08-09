CREATE TABLE network.records (
    uri                 TEXT PRIMARY KEY,
    cid                 TEXT,
    author_did          TEXT NOT NULL,
    collection          TEXT NOT NULL,
    rkey                TEXT NOT NULL,
    record              JSONB,
    record_created_at   TIMESTAMPTZ,
    indexed_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ,
    source_event_id     BIGINT NOT NULL,
    CONSTRAINT records_event_id_positive CHECK (source_event_id > 0),
    CONSTRAINT records_live_value CHECK (deleted_at IS NOT NULL OR (cid IS NOT NULL AND record IS NOT NULL))
);

CREATE INDEX network_records_author_idx ON network.records (author_did, collection);
CREATE INDEX network_records_collection_idx ON network.records (collection, indexed_at DESC);

CREATE TABLE network.identities (
    did                 TEXT PRIMARY KEY,
    handle              TEXT,
    status              TEXT NOT NULL,
    is_active           BOOLEAN NOT NULL,
    indexed_at          TIMESTAMPTZ NOT NULL,
    source_event_id     BIGINT NOT NULL,
    CONSTRAINT identities_event_id_positive CHECK (source_event_id > 0)
);

CREATE INDEX network_identities_handle_idx ON network.identities (lower(handle));

CREATE TABLE network.repositories (
    uri                 TEXT PRIMARY KEY,
    cid                 TEXT,
    owner_did           TEXT NOT NULL,
    rkey                TEXT NOT NULL,
    slug                TEXT,
    name                TEXT,
    description         TEXT,
    default_branch      TEXT,
    git_https           TEXT,
    git_ssh             TEXT,
    web                 TEXT,
    local_repository_id UUID REFERENCES core.repositories(id) ON DELETE SET NULL,
    record_created_at   TIMESTAMPTZ,
    record_updated_at   TIMESTAMPTZ,
    indexed_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ,
    source_event_id     BIGINT NOT NULL,
    CONSTRAINT network_repositories_event_id_positive CHECK (source_event_id > 0),
    CONSTRAINT network_repositories_live_value CHECK (deleted_at IS NOT NULL OR cid IS NOT NULL)
);

CREATE INDEX network_repositories_owner_idx ON network.repositories (owner_did, slug) WHERE deleted_at IS NULL;
CREATE INDEX network_repositories_local_idx ON network.repositories (local_repository_id) WHERE local_repository_id IS NOT NULL;

ALTER TABLE network.profiles
    ADD COLUMN deleted_at TIMESTAMPTZ,
    ADD COLUMN source_event_id BIGINT,
    ADD COLUMN handle_source_event_id BIGINT,
    ADD CONSTRAINT profiles_source_event_id_positive CHECK (source_event_id IS NULL OR source_event_id > 0),
    ADD CONSTRAINT profiles_handle_source_event_id_positive CHECK (handle_source_event_id IS NULL OR handle_source_event_id > 0);

CREATE TABLE ops.federation_cursors (
    consumer            TEXT PRIMARY KEY,
    event_id            BIGINT NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    CONSTRAINT federation_cursors_event_id_positive CHECK (event_id > 0)
);

CREATE TABLE ops.federation_receipts (
    consumer            TEXT NOT NULL,
    event_id            BIGINT NOT NULL,
    outcome             TEXT NOT NULL,
    rejection           TEXT,
    received_at         TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (consumer, event_id),
    CONSTRAINT federation_receipts_event_id_positive CHECK (event_id > 0),
    CONSTRAINT federation_receipts_outcome CHECK (outcome IN ('applied', 'rejected'))
);
