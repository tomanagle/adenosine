CREATE TABLE core.repository_transfers (
    id UUID PRIMARY KEY,
    repository_id UUID NOT NULL REFERENCES core.repositories(id) ON DELETE CASCADE,
    source_owner_did TEXT NOT NULL REFERENCES core.accounts(did),
    source_organization_id UUID REFERENCES core.organizations(id),
    source_owner_alias TEXT NOT NULL,
    source_repository_uri TEXT,
    source_repository_cid TEXT,
    destination_owner_did TEXT NOT NULL REFERENCES core.accounts(did),
    destination_organization_id UUID REFERENCES core.organizations(id),
    destination_owner_alias TEXT NOT NULL,
    initiated_by_did TEXT NOT NULL REFERENCES core.accounts(did),
    accepted_by_did TEXT REFERENCES core.accounts(did),
    proposal_uri TEXT,
    proposal_cid TEXT,
    successor_uri TEXT,
    successor_cid TEXT,
    acceptance_uri TEXT,
    acceptance_cid TEXT,
    source_redirect_cid TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    acceptance_started_at TIMESTAMPTZ,
    accepted_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    CONSTRAINT repository_transfers_status CHECK (status IN ('pending', 'completed', 'cancelled')),
    CONSTRAINT repository_transfers_expiry CHECK (expires_at > created_at),
    CONSTRAINT repository_transfers_source_identity_pair CHECK ((source_repository_uri IS NULL) = (source_repository_cid IS NULL)),
    CONSTRAINT repository_transfers_proposal_pair CHECK ((proposal_uri IS NULL) = (proposal_cid IS NULL)),
    CONSTRAINT repository_transfers_successor_pair CHECK ((successor_uri IS NULL) = (successor_cid IS NULL)),
    CONSTRAINT repository_transfers_acceptance_pair CHECK ((acceptance_uri IS NULL) = (acceptance_cid IS NULL)),
    CONSTRAINT repository_transfers_acceptance_window CHECK (
        acceptance_started_at IS NULL
        OR (acceptance_started_at >= created_at AND acceptance_started_at < expires_at)
    ),
    CONSTRAINT repository_transfers_terminal_state CHECK (
        (status = 'pending' AND accepted_by_did IS NULL AND accepted_at IS NULL AND cancelled_at IS NULL)
        OR (status = 'completed' AND acceptance_started_at IS NOT NULL AND accepted_by_did IS NOT NULL
            AND accepted_at IS NOT NULL AND accepted_at >= acceptance_started_at AND cancelled_at IS NULL)
        OR (status = 'cancelled' AND acceptance_started_at IS NULL AND accepted_by_did IS NULL
            AND accepted_at IS NULL AND cancelled_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX repository_transfers_pending_uidx
    ON core.repository_transfers (repository_id)
    WHERE status = 'pending';

CREATE INDEX repository_transfers_history_idx
    ON core.repository_transfers (repository_id, created_at DESC, id DESC);

CREATE INDEX repository_transfers_destination_pending_idx
    ON core.repository_transfers (destination_owner_did, created_at DESC, id DESC)
    WHERE status = 'pending';

ALTER TABLE core.repositories
    ADD COLUMN transferred_from_uri TEXT,
    ADD COLUMN transferred_from_cid TEXT,
    ADD CONSTRAINT core_repositories_transferred_from_pair CHECK ((transferred_from_uri IS NULL) = (transferred_from_cid IS NULL));

ALTER TABLE network.repositories
    ADD COLUMN transferred_from_uri TEXT,
    ADD COLUMN transferred_from_cid TEXT,
    ADD COLUMN transferred_to_uri TEXT,
    ADD COLUMN transferred_to_cid TEXT,
    ADD COLUMN lineage_uri TEXT,
    ADD COLUMN canonical_uri TEXT,
    ADD CONSTRAINT network_repositories_transferred_from_pair CHECK ((transferred_from_uri IS NULL) = (transferred_from_cid IS NULL)),
    ADD CONSTRAINT network_repositories_transferred_to_pair CHECK ((transferred_to_uri IS NULL) = (transferred_to_cid IS NULL));

UPDATE network.repositories SET lineage_uri = uri, canonical_uri = uri;

ALTER TABLE network.repositories
    ALTER COLUMN lineage_uri SET NOT NULL,
    ALTER COLUMN canonical_uri SET NOT NULL;

CREATE INDEX network_repositories_lineage_idx ON network.repositories (lineage_uri) WHERE deleted_at IS NULL;
CREATE INDEX network_repositories_canonical_idx ON network.repositories (canonical_uri) WHERE deleted_at IS NULL;

CREATE TABLE network.repository_transfers (
    uri TEXT PRIMARY KEY,
    cid TEXT,
    author_did TEXT NOT NULL,
    rkey TEXT NOT NULL,
    repository_uri TEXT,
    repository_cid TEXT,
    destination_did TEXT,
    destination_organization_uri TEXT,
    destination_organization_cid TEXT,
    destination_owner_alias TEXT,
    created_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    indexed_at TIMESTAMPTZ NOT NULL,
    source_event_id BIGINT NOT NULL,
    deleted_at TIMESTAMPTZ,
    CONSTRAINT network_repository_transfer_destination CHECK (
        destination_did IS NOT NULL
        AND ((destination_organization_uri IS NULL AND destination_organization_cid IS NULL)
          OR (destination_organization_uri IS NOT NULL AND destination_organization_cid IS NOT NULL))
    )
);

CREATE TABLE network.repository_transfer_acceptances (
    uri TEXT PRIMARY KEY,
    cid TEXT,
    author_did TEXT NOT NULL,
    rkey TEXT NOT NULL,
    proposal_uri TEXT,
    proposal_cid TEXT,
    repository_uri TEXT,
    repository_cid TEXT,
    created_at TIMESTAMPTZ,
    indexed_at TIMESTAMPTZ NOT NULL,
    source_event_id BIGINT NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX network_repository_transfers_repository_idx
    ON network.repository_transfers (repository_uri)
    WHERE deleted_at IS NULL;

CREATE INDEX network_repository_transfer_acceptances_proposal_idx
    ON network.repository_transfer_acceptances (proposal_uri)
    WHERE deleted_at IS NULL;
