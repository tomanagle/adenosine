CREATE TABLE core.repository_webhooks (
    id UUID PRIMARY KEY,
    repository_id UUID NOT NULL REFERENCES core.repositories(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    secret_ciphertext BYTEA NOT NULL,
    events TEXT[] NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ,
    CONSTRAINT repository_webhooks_events_nonempty CHECK (cardinality(events) > 0)
);

CREATE INDEX repository_webhooks_repository_idx
    ON core.repository_webhooks (repository_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE ops.webhook_deliveries (
    id UUID PRIMARY KEY,
    webhook_id UUID NOT NULL REFERENCES core.repository_webhooks(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    event_id UUID NOT NULL,
    request_body JSONB NOT NULL,
    response_status INTEGER,
    response_body TEXT,
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL,
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    delivered_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    last_error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT webhook_deliveries_attempts_nonnegative CHECK (attempts >= 0),
    CONSTRAINT webhook_deliveries_response_status CHECK (
        response_status IS NULL OR response_status BETWEEN 100 AND 599
    ),
    UNIQUE (webhook_id, event_id)
);

CREATE INDEX webhook_deliveries_ready_idx
    ON ops.webhook_deliveries (available_at, created_at, id)
    WHERE delivered_at IS NULL AND failed_at IS NULL;

CREATE INDEX webhook_deliveries_webhook_idx
    ON ops.webhook_deliveries (webhook_id, created_at DESC, id DESC);
