CREATE TABLE auth.oauth_states (
    state_hash BYTEA PRIMARY KEY,
    encrypted_payload BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX oauth_states_expiry_idx ON auth.oauth_states (expires_at);
