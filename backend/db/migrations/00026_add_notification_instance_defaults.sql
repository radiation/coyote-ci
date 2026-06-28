-- +goose Up

CREATE TABLE IF NOT EXISTS notification_instance_settings (
	singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton = TRUE),
	default_commit_author_failure_email_enabled BOOLEAN NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE user_notification_preferences
	ADD COLUMN IF NOT EXISTS source TEXT;

UPDATE user_notification_preferences
SET source = 'user'
WHERE source IS NULL;

ALTER TABLE user_notification_preferences
	ALTER COLUMN source SET NOT NULL;

ALTER TABLE user_notification_preferences
	DROP CONSTRAINT IF EXISTS user_notification_preferences_source_check;

ALTER TABLE user_notification_preferences
	ADD CONSTRAINT user_notification_preferences_source_check
	CHECK (source IN ('instance_default', 'user'));

-- +goose Down

ALTER TABLE user_notification_preferences
	DROP CONSTRAINT IF EXISTS user_notification_preferences_source_check;

ALTER TABLE user_notification_preferences
	DROP COLUMN IF EXISTS source;

DROP TABLE IF EXISTS notification_instance_settings;