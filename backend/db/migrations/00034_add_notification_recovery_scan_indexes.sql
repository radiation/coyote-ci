-- +goose Up

-- The recovery drain runs periodically on every server process. Even with a
-- small LIMIT, the sweep must avoid repeated table scans while ordering due
-- retries and expired claims deterministically.
CREATE INDEX IF NOT EXISTS idx_notification_deliveries_retry_waiting_next_attempt_at_id
    ON notification_deliveries (next_attempt_at, id)
    WHERE status = 'retry_waiting';

DROP INDEX IF EXISTS idx_notification_deliveries_retry_waiting_next_attempt_at;

COMMENT ON INDEX idx_notification_deliveries_retry_waiting_next_attempt_at_id IS 'Supports bounded periodic recovery scans for due notification retries ordered by next_attempt_at and id across all servers.';

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_sending_claim_expires_at_id
    ON notification_deliveries (claim_expires_at, id)
    WHERE status = 'sending';

DROP INDEX IF EXISTS idx_notification_deliveries_sending_claim_expires_at;

COMMENT ON INDEX idx_notification_deliveries_sending_claim_expires_at_id IS 'Supports bounded periodic recovery scans for expired notification claims ordered by claim_expires_at and id across all servers.';

-- +goose Down

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_retry_waiting_next_attempt_at
    ON notification_deliveries (next_attempt_at)
    WHERE status = 'retry_waiting';

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_sending_claim_expires_at
    ON notification_deliveries (claim_expires_at)
    WHERE status = 'sending';

DROP INDEX IF EXISTS idx_notification_deliveries_sending_claim_expires_at_id;
DROP INDEX IF EXISTS idx_notification_deliveries_retry_waiting_next_attempt_at_id;