CREATE TABLE network.profiles (
    did                 TEXT PRIMARY KEY,
    profile_uri         TEXT UNIQUE,
    profile_cid         TEXT,

    handle              TEXT,
    display_name        TEXT,
    bio                 TEXT,
    avatar_ref          TEXT,
    website             TEXT,
    location            TEXT,

    repository_count    BIGINT NOT NULL DEFAULT 0,
    contribution_count  BIGINT NOT NULL DEFAULT 0,

    record_created_at   TIMESTAMPTZ,
    indexed_at          TIMESTAMPTZ NOT NULL,

    CONSTRAINT profiles_did_nonempty CHECK (length(did) > 0),
    CONSTRAINT profiles_repository_count_nonnegative CHECK (repository_count >= 0),
    CONSTRAINT profiles_contribution_count_nonnegative CHECK (contribution_count >= 0)
);

CREATE INDEX network_profiles_handle_idx
    ON network.profiles (lower(handle));

CREATE INDEX network_profiles_display_name_idx
    ON network.profiles (lower(display_name));
