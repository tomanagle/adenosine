CREATE TABLE core.branch_protections (
    id UUID PRIMARY KEY,
    repository_id UUID NOT NULL REFERENCES core.repositories(id) ON DELETE CASCADE,
    pattern TEXT NOT NULL,
    deny_force_push BOOLEAN NOT NULL DEFAULT TRUE,
    deny_deletion BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT branch_protections_pattern_nonempty CHECK (length(pattern) BETWEEN 1 AND 255),
    UNIQUE (repository_id, pattern)
);

CREATE INDEX branch_protections_repository_idx
    ON core.branch_protections (repository_id, created_at DESC, id DESC);
