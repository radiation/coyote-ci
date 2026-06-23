-- +goose Up

ALTER TABLE notification_targets
    DROP CONSTRAINT IF EXISTS notification_targets_type_check;

ALTER TABLE notification_targets
    ADD CONSTRAINT notification_targets_type_check CHECK (type IN ('email', 'slack_webhook'));

-- +goose Down

ALTER TABLE notification_targets
    DROP CONSTRAINT IF EXISTS notification_targets_type_check;

ALTER TABLE notification_targets
    ADD CONSTRAINT notification_targets_type_check CHECK (type IN ('email'));