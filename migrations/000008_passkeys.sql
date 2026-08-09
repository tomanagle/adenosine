CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE auth.webauthn_users (
    rp_id TEXT NOT NULL,
    account_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE CASCADE,
    handle BYTEA NOT NULL DEFAULT gen_random_bytes(32),
    name TEXT NOT NULL,
    display_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (rp_id, account_did),
    CONSTRAINT webauthn_users_handle_unique UNIQUE (rp_id, handle),
    CONSTRAINT webauthn_users_rp_id_nonempty CHECK (length(rp_id) > 0),
    CONSTRAINT webauthn_users_handle_length CHECK (octet_length(handle) = 32),
    CONSTRAINT webauthn_users_name_nonempty CHECK (length(name) > 0),
    CONSTRAINT webauthn_users_display_name_nonempty CHECK (length(display_name) > 0)
);

CREATE TABLE auth.passkey_credentials (
    id UUID PRIMARY KEY,
    rp_id TEXT NOT NULL,
    account_did TEXT NOT NULL,
    name TEXT NOT NULL,
    credential_id BYTEA NOT NULL UNIQUE,
    public_key BYTEA NOT NULL,
    attestation_type TEXT NOT NULL,
    transports TEXT[] NOT NULL DEFAULT '{}',
    flags SMALLINT NOT NULL,
    aaguid BYTEA NOT NULL,
    sign_count BIGINT NOT NULL,
    clone_warning BOOLEAN NOT NULL DEFAULT false,
    attachment TEXT NOT NULL,
    attestation_client_data_json BYTEA NOT NULL,
    attestation_client_data_hash BYTEA NOT NULL,
    attestation_authenticator_data BYTEA NOT NULL,
    attestation_public_key_algorithm BIGINT NOT NULL,
    attestation_object BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT passkey_credentials_user_fk
        FOREIGN KEY (rp_id, account_did)
        REFERENCES auth.webauthn_users(rp_id, account_did)
        ON DELETE CASCADE,
    CONSTRAINT passkey_credentials_name_nonempty CHECK (length(name) > 0),
    CONSTRAINT passkey_credentials_id_nonempty CHECK (octet_length(credential_id) > 0),
    CONSTRAINT passkey_credentials_public_key_nonempty CHECK (octet_length(public_key) > 0),
    CONSTRAINT passkey_credentials_flags_byte CHECK (flags BETWEEN 0 AND 255),
    CONSTRAINT passkey_credentials_aaguid_length CHECK (octet_length(aaguid) = 16),
    CONSTRAINT passkey_credentials_sign_count_uint32 CHECK (sign_count BETWEEN 0 AND 4294967295),
    CONSTRAINT passkey_credentials_attachment CHECK (attachment IN ('', 'platform', 'cross-platform'))
);

CREATE INDEX passkey_credentials_account_active_idx
    ON auth.passkey_credentials (rp_id, account_did, created_at DESC, id DESC)
    WHERE revoked_at IS NULL;

CREATE TABLE auth.passkey_ceremonies (
    token_hash BYTEA PRIMARY KEY,
    kind TEXT NOT NULL,
    rp_id TEXT NOT NULL,
    account_did TEXT REFERENCES core.accounts(did) ON DELETE CASCADE,
    browser_session_id UUID REFERENCES auth.sessions(id) ON DELETE CASCADE,
    session_data BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT passkey_ceremonies_token_hash_nonempty CHECK (octet_length(token_hash) > 0),
    CONSTRAINT passkey_ceremonies_kind CHECK (kind IN ('registration', 'authentication')),
    CONSTRAINT passkey_ceremonies_rp_id_nonempty CHECK (length(rp_id) > 0),
    CONSTRAINT passkey_ceremonies_expiry CHECK (expires_at > created_at),
    CONSTRAINT passkey_ceremonies_binding CHECK (
        (kind = 'registration' AND account_did IS NOT NULL AND browser_session_id IS NOT NULL)
        OR (kind = 'authentication' AND account_did IS NULL AND browser_session_id IS NULL)
    )
);

CREATE INDEX passkey_ceremonies_expiry_idx ON auth.passkey_ceremonies (expires_at);
CREATE INDEX passkey_ceremonies_account_idx ON auth.passkey_ceremonies (rp_id, account_did, expires_at);
