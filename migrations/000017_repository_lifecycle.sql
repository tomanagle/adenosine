ALTER TABLE core.repositories
    ADD COLUMN archived_at TIMESTAMPTZ;

CREATE TABLE core.repository_deletions (
    id UUID PRIMARY KEY,
    repository_id UUID NOT NULL UNIQUE REFERENCES core.repositories(id) ON DELETE CASCADE,
    requested_by_did TEXT NOT NULL REFERENCES core.accounts(did),
    requested_at TIMESTAMPTZ NOT NULL,
    purge_after TIMESTAMPTZ NOT NULL,
    restored_at TIMESTAMPTZ,
    purged_at TIMESTAMPTZ,
    CONSTRAINT repository_deletions_retention CHECK (purge_after > requested_at),
    CONSTRAINT repository_deletions_terminal_state CHECK (
        restored_at IS NULL OR purged_at IS NULL
    )
);

CREATE INDEX repository_deletions_due_idx
    ON core.repository_deletions (purge_after, id)
    WHERE restored_at IS NULL AND purged_at IS NULL;
