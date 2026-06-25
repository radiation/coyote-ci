-- +goose Up

ALTER TABLE notification_targets
    ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_notification_targets_owner_user_id
    ON notification_targets (owner_user_id);

CREATE UNIQUE INDEX IF NOT EXISTS notification_targets_owner_user_email_key
    ON notification_targets (owner_user_id)
    WHERE owner_user_id IS NOT NULL AND type = 'email';

-- +goose Down

DROP INDEX IF EXISTS notification_targets_owner_user_email_key;
DROP INDEX IF EXISTS idx_notification_targets_owner_user_id;
ALTER TABLE notification_targets DROP COLUMN IF EXISTS owner_user_id;
