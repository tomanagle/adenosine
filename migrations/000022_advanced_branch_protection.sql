ALTER TABLE core.branch_protections
    ADD COLUMN required_approvals SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN dismiss_stale_reviews BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN required_status_checks TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN require_signed_commits BOOLEAN NOT NULL DEFAULT FALSE,
    ADD CONSTRAINT branch_protections_required_approvals_range
        CHECK (required_approvals BETWEEN 0 AND 100),
    ADD CONSTRAINT branch_protections_required_status_checks_limit
        CHECK (cardinality(required_status_checks) <= 50);

