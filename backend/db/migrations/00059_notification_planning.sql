-- +goose Up
ALTER TABLE builds ADD COLUMN notification_plan_complete boolean NOT NULL DEFAULT true;
ALTER TABLE builds ALTER COLUMN notification_plan_complete SET DEFAULT false;
UPDATE builds SET notification_plan_complete = false WHERE status NOT IN ('success', 'failed', 'canceled');
CREATE INDEX builds_unplanned_notifications ON builds (id)
    WHERE NOT notification_plan_complete AND status IN ('success', 'failed');
CREATE INDEX notification_deliveries_pending ON notification_deliveries (created_at, id) WHERE status = 'pending';

-- +goose Down
DROP INDEX notification_deliveries_pending;
DROP INDEX builds_unplanned_notifications;
ALTER TABLE builds DROP COLUMN notification_plan_complete;