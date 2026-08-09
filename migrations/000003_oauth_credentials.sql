CREATE TABLE auth.oauth_credentials (
    account_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE CASCADE,
    session_id_hash BYTEA NOT NULL,
    encrypted_payload BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (account_did, session_id_hash),
    CONSTRAINT oauth_credentials_session_id_hash_length CHECK (octet_length(session_id_hash) = 32)
);
