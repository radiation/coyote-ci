-- +goose Up

ALTER TABLE notification_deliveries
    ADD COLUMN IF NOT EXISTS max_attempts INTEGER,
    ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS claimed_by TEXT,
    ADD COLUMN IF NOT EXISTS failure_category TEXT,
    ADD COLUMN IF NOT EXISTS failure_reason TEXT;

UPDATE notification_deliveries
SET attempts = CASE
        WHEN status IN ('sent', 'failed', 'pending') THEN GREATEST(attempts, 1)
        ELSE attempts
    END,
    max_attempts = CASE
        WHEN status = 'sent' THEN GREATEST(attempts, 1)
        WHEN status = 'failed' THEN GREATEST(attempts, 1)
        WHEN status = 'pending' THEN GREATEST(attempts, 1)
        ELSE COALESCE(max_attempts, 1)
    END,
    last_attempt_at = CASE
        WHEN status = 'sent' THEN COALESCE(sent_at, updated_at, created_at)
        WHEN status = 'failed' THEN COALESCE(updated_at, created_at)
        WHEN status = 'pending' THEN COALESCE(updated_at, created_at)
        ELSE last_attempt_at
    END,
    next_attempt_at = NULL,
    claimed_at = NULL,
    claim_expires_at = NULL,
    claimed_by = NULL,
    failure_category = CASE
        WHEN status = 'failed' THEN 'permanent'
        WHEN status = 'pending' THEN 'canceled'
        ELSE NULL
    END,
    failure_reason = CASE
        WHEN status = 'failed' THEN 'legacy_failed_no_retry'
        WHEN status = 'pending' THEN 'legacy_pending_no_automatic_retry'
        ELSE NULL
    END,
    status = CASE
        WHEN status = 'sent' THEN 'sent'
        WHEN status = 'failed' THEN 'failed_permanent'
        WHEN status = 'pending' THEN 'failed_exhausted'
        ELSE status
    END;

ALTER TABLE notification_deliveries
    ALTER COLUMN max_attempts SET NOT NULL,
    ALTER COLUMN max_attempts SET DEFAULT 1;

ALTER TABLE notification_deliveries
    DROP CONSTRAINT IF EXISTS notification_deliveries_status_check;

ALTER TABLE notification_deliveries
    DROP CONSTRAINT IF EXISTS notification_deliveries_attempts_check;

ALTER TABLE notification_deliveries
    ADD CONSTRAINT notification_deliveries_status_check
        CHECK (status IN ('pending', 'sending', 'retry_waiting', 'sent', 'failed_permanent', 'failed_exhausted')),
    ADD CONSTRAINT notification_deliveries_attempts_check
        CHECK (attempts >= 0),
    ADD CONSTRAINT notification_deliveries_max_attempts_check
        CHECK (max_attempts > 0),
    ADD CONSTRAINT notification_deliveries_failure_category_check
        CHECK (failure_category IS NULL OR failure_category IN ('retryable', 'permanent', 'canceled')),
    ADD CONSTRAINT notification_deliveries_claim_retry_state_check
        CHECK (
            (status = 'pending'
                AND claimed_at IS NULL
                AND claim_expires_at IS NULL
                AND claimed_by IS NULL
                AND next_attempt_at IS NULL
                AND sent_at IS NULL)
            OR (status = 'sending'
                AND claimed_at IS NOT NULL
                AND claim_expires_at IS NOT NULL
                AND claimed_by IS NOT NULL
                AND claim_expires_at > claimed_at
                AND next_attempt_at IS NULL
                AND sent_at IS NULL)
            OR (status = 'retry_waiting'
                AND claimed_at IS NULL
                AND claim_expires_at IS NULL
                AND claimed_by IS NULL
                AND next_attempt_at IS NOT NULL
                AND sent_at IS NULL)
            OR (status = 'sent'
                AND claimed_at IS NULL
                AND claim_expires_at IS NULL
                AND claimed_by IS NULL
                AND next_attempt_at IS NULL
                AND sent_at IS NOT NULL)
            OR (status = 'failed_permanent'
                AND claimed_at IS NULL
                AND claim_expires_at IS NULL
                AND claimed_by IS NULL
                AND next_attempt_at IS NULL
                AND sent_at IS NULL)
            OR (status = 'failed_exhausted'
                AND claimed_at IS NULL
                AND claim_expires_at IS NULL
                AND claimed_by IS NULL
                AND next_attempt_at IS NULL
                AND sent_at IS NULL
                AND attempts >= max_attempts)
        );

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_retry_waiting_next_attempt_at
    ON notification_deliveries (next_attempt_at)
    WHERE status = 'retry_waiting';

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_sending_claim_expires_at
    ON notification_deliveries (claim_expires_at)
    WHERE status = 'sending';

-- +goose Down

DROP INDEX IF EXISTS idx_notification_deliveries_sending_claim_expires_at;
DROP INDEX IF EXISTS idx_notification_deliveries_retry_waiting_next_attempt_at;

ALTER TABLE notification_deliveries
    DROP CONSTRAINT IF EXISTS notification_deliveries_claim_retry_state_check,
    DROP CONSTRAINT IF EXISTS notification_deliveries_failure_category_check,
    DROP CONSTRAINT IF EXISTS notification_deliveries_max_attempts_check,
    DROP CONSTRAINT IF EXISTS notification_deliveries_attempts_check,
    DROP CONSTRAINT IF EXISTS notification_deliveries_status_check;

ALTER TABLE notification_deliveries
    ADD CONSTRAINT notification_deliveries_status_check
        CHECK (status IN ('pending', 'sent', 'failed')),
    ADD CONSTRAINT notification_deliveries_attempts_check
        CHECK (attempts >= 0);

ALTER TABLE notification_deliveries
    DROP COLUMN IF EXISTS failure_reason,
    DROP COLUMN IF EXISTS failure_category,
    DROP COLUMN IF EXISTS claimed_by,
    DROP COLUMN IF EXISTS claim_expires_at,
    DROP COLUMN IF EXISTS claimed_at,
    DROP COLUMN IF EXISTS next_attempt_at,
    DROP COLUMN IF EXISTS last_attempt_at,
    DROP COLUMN IF EXISTS max_attempts;