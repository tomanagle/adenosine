CREATE TABLE network.repository_labels (
    uri               TEXT PRIMARY KEY,
    cid               TEXT,
    author_did        TEXT NOT NULL,
    rkey              TEXT NOT NULL,
    repository_uri    TEXT NOT NULL,
    repository_cid    TEXT NOT NULL,
    name              TEXT NOT NULL,
    color             TEXT NOT NULL,
    description       TEXT NOT NULL,
    record_created_at TIMESTAMPTZ NOT NULL,
    record_updated_at TIMESTAMPTZ NOT NULL,
    indexed_at        TIMESTAMPTZ NOT NULL,
    deleted_at        TIMESTAMPTZ,
    source_event_id   BIGINT NOT NULL,
    CONSTRAINT network_repository_labels_event_id_positive CHECK (source_event_id > 0),
    CONSTRAINT network_repository_labels_name_length CHECK (length(name) BETWEEN 1 AND 50),
    CONSTRAINT network_repository_labels_color CHECK (color ~ '^[0-9a-f]{6}$'),
    CONSTRAINT network_repository_labels_description_length CHECK (length(description) <= 255),
    CONSTRAINT network_repository_labels_live_value CHECK (deleted_at IS NOT NULL OR cid IS NOT NULL)
);

CREATE INDEX network_repository_labels_repository_idx
    ON network.repository_labels (repository_uri, record_created_at DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX network_repository_labels_name_idx
    ON network.repository_labels (repository_uri, lower(name), uri)
    WHERE deleted_at IS NULL;

CREATE TABLE network.repository_milestones (
    uri               TEXT PRIMARY KEY,
    cid               TEXT,
    author_did        TEXT NOT NULL,
    rkey              TEXT NOT NULL,
    repository_uri    TEXT NOT NULL,
    repository_cid    TEXT NOT NULL,
    title             TEXT NOT NULL,
    description       TEXT NOT NULL,
    state             TEXT NOT NULL,
    due_at            TIMESTAMPTZ,
    closed_at         TIMESTAMPTZ,
    record_created_at TIMESTAMPTZ NOT NULL,
    record_updated_at TIMESTAMPTZ NOT NULL,
    indexed_at        TIMESTAMPTZ NOT NULL,
    deleted_at        TIMESTAMPTZ,
    source_event_id   BIGINT NOT NULL,
    CONSTRAINT network_repository_milestones_event_id_positive CHECK (source_event_id > 0),
    CONSTRAINT network_repository_milestones_title_length CHECK (length(title) BETWEEN 1 AND 255),
    CONSTRAINT network_repository_milestones_description_length CHECK (length(description) <= 65535),
    CONSTRAINT network_repository_milestones_state CHECK (state IN ('open', 'closed')),
    CONSTRAINT network_repository_milestones_closed_shape CHECK (
        (state = 'open' AND closed_at IS NULL)
        OR (state = 'closed' AND closed_at IS NOT NULL)
    ),
    CONSTRAINT network_repository_milestones_live_value CHECK (deleted_at IS NOT NULL OR cid IS NOT NULL)
);

CREATE INDEX network_repository_milestones_repository_idx
    ON network.repository_milestones (repository_uri, record_created_at DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX network_repository_milestones_state_idx
    ON network.repository_milestones (repository_uri, state, due_at, uri)
    WHERE deleted_at IS NULL;

CREATE TABLE network.subject_triage (
    uri               TEXT PRIMARY KEY,
    cid               TEXT,
    author_did        TEXT NOT NULL,
    rkey              TEXT NOT NULL,
    subject_uri       TEXT NOT NULL,
    subject_cid       TEXT NOT NULL,
    subject_kind      TEXT NOT NULL,
    repository_uri    TEXT NOT NULL,
    repository_cid    TEXT NOT NULL,
    label_uris        TEXT[] NOT NULL DEFAULT '{}',
    assignee_dids     TEXT[] NOT NULL DEFAULT '{}',
    milestone_uri     TEXT,
    record_created_at TIMESTAMPTZ NOT NULL,
    record_updated_at TIMESTAMPTZ NOT NULL,
    indexed_at        TIMESTAMPTZ NOT NULL,
    deleted_at        TIMESTAMPTZ,
    source_event_id   BIGINT NOT NULL,
    CONSTRAINT network_subject_triage_event_id_positive CHECK (source_event_id > 0),
    CONSTRAINT network_subject_triage_kind CHECK (subject_kind IN ('issue', 'pull_request')),
    CONSTRAINT network_subject_triage_label_limit CHECK (cardinality(label_uris) <= 20),
    CONSTRAINT network_subject_triage_assignee_limit CHECK (cardinality(assignee_dids) <= 10),
    CONSTRAINT network_subject_triage_label_values CHECK (array_position(label_uris, NULL) IS NULL),
    CONSTRAINT network_subject_triage_assignee_values CHECK (array_position(assignee_dids, NULL) IS NULL),
    CONSTRAINT network_subject_triage_live_value CHECK (deleted_at IS NOT NULL OR cid IS NOT NULL)
);

CREATE INDEX network_subject_triage_subject_idx
    ON network.subject_triage (subject_uri, source_event_id DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX network_subject_triage_repository_idx
    ON network.subject_triage (repository_uri, subject_kind, source_event_id DESC, uri DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX network_subject_triage_labels_idx
    ON network.subject_triage USING GIN (label_uris)
    WHERE deleted_at IS NULL;

CREATE INDEX network_subject_triage_assignees_idx
    ON network.subject_triage USING GIN (assignee_dids)
    WHERE deleted_at IS NULL;

CREATE INDEX network_subject_triage_milestone_idx
    ON network.subject_triage (milestone_uri, subject_kind, subject_uri)
    WHERE deleted_at IS NULL AND milestone_uri IS NOT NULL;
