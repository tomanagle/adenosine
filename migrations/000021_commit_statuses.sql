CREATE TABLE core.commit_statuses (
    id UUID PRIMARY KEY,
    repository_id UUID NOT NULL REFERENCES core.repositories(id) ON DELETE CASCADE,
    commit_sha TEXT NOT NULL,
    context TEXT NOT NULL,
    state TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    target_url TEXT,
    creator_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE RESTRICT,
    external_id TEXT NOT NULL,
    request_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT commit_statuses_sha CHECK (commit_sha ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
    CONSTRAINT commit_statuses_context CHECK (length(context) BETWEEN 1 AND 100),
    CONSTRAINT commit_statuses_state CHECK (state IN ('pending', 'success', 'failure', 'error')),
    CONSTRAINT commit_statuses_description CHECK (length(description) <= 140),
    CONSTRAINT commit_statuses_external_id CHECK (length(external_id) BETWEEN 1 AND 255),
    CONSTRAINT commit_statuses_expiry CHECK (expires_at > created_at),
    UNIQUE (repository_id, creator_did, external_id)
);

CREATE INDEX commit_statuses_commit_history_idx
    ON core.commit_statuses (repository_id, commit_sha, id DESC);

CREATE INDEX commit_statuses_commit_context_idx
    ON core.commit_statuses (repository_id, commit_sha, context, id DESC);

CREATE INDEX commit_statuses_expiry_idx
    ON core.commit_statuses (expires_at, id);

CREATE TABLE core.check_runs (
    id UUID PRIMARY KEY,
    repository_id UUID NOT NULL REFERENCES core.repositories(id) ON DELETE CASCADE,
    commit_sha TEXT NOT NULL,
    name TEXT NOT NULL,
    external_id TEXT NOT NULL,
    creator_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE RESTRICT,
    status TEXT NOT NULL,
    conclusion TEXT,
    details_url TEXT,
    output_title TEXT NOT NULL DEFAULT '',
    output_summary TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    create_request_hash BYTEA NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT check_runs_sha CHECK (commit_sha ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
    CONSTRAINT check_runs_name CHECK (length(name) BETWEEN 1 AND 100),
    CONSTRAINT check_runs_external_id CHECK (length(external_id) BETWEEN 1 AND 255),
    CONSTRAINT check_runs_status CHECK (status IN ('queued', 'in_progress', 'completed')),
    CONSTRAINT check_runs_conclusion CHECK (conclusion IS NULL OR conclusion IN ('success', 'failure', 'neutral', 'cancelled', 'skipped', 'timed_out', 'action_required')),
    CONSTRAINT check_runs_output_title CHECK (length(output_title) <= 255),
    CONSTRAINT check_runs_output_summary CHECK (length(output_summary) <= 65535),
    CONSTRAINT check_runs_version CHECK (version > 0),
    CONSTRAINT check_runs_lifecycle CHECK (
        (status = 'completed' AND conclusion IS NOT NULL AND completed_at IS NOT NULL)
        OR (status <> 'completed' AND conclusion IS NULL AND completed_at IS NULL)
    ),
    CONSTRAINT check_runs_expiry CHECK (expires_at > created_at),
    UNIQUE (repository_id, creator_did, external_id)
);

CREATE INDEX check_runs_commit_history_idx
    ON core.check_runs (repository_id, commit_sha, id DESC);

CREATE INDEX check_runs_commit_name_idx
    ON core.check_runs (repository_id, commit_sha, name, id DESC);

CREATE INDEX check_runs_expiry_idx
    ON core.check_runs (expires_at, id);
