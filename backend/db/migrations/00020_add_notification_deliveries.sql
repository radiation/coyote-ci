-- +goose Up

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id TEXT PRIMARY KEY,
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    recipient TEXT NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    CONSTRAINT notification_deliveries_build_event_recipient_key UNIQUE (build_id, event_type, recipient),
    CONSTRAINT notification_deliveries_status_check CHECK (status IN ('pending', 'sent', 'failed')),
    CONSTRAINT notification_deliveries_attempts_check CHECK (attempts >= 0)
);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_build_id
    ON notification_deliveries (build_id);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_status
    ON notification_deliveries (status);

-- +goose Down

DROP INDEX IF EXISTS idx_notification_deliveries_status;
DROP INDEX IF EXISTS idx_notification_deliveries_build_id;
DROP TABLE IF EXISTS notification_deliveries;