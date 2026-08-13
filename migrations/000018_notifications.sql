CREATE TABLE core.notification_states (
    account_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE CASCADE,
    notification_key UUID NOT NULL,
    read_at TIMESTAMPTZ,
    dismissed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (account_did, notification_key)
);

CREATE INDEX notification_states_account_unread_idx
    ON core.notification_states (account_did, updated_at DESC)
    WHERE read_at IS NULL AND dismissed_at IS NULL;
