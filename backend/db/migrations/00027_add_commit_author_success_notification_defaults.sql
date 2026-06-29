-- +goose Up

ALTER TABLE notification_instance_settings
	ADD COLUMN IF NOT EXISTS default_commit_author_success_email_enabled BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE user_notification_preferences
	ADD COLUMN IF NOT EXISTS commit_author_success_enabled BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE user_notification_preferences
	ADD COLUMN IF NOT EXISTS commit_author_success_source TEXT;

ALTER TABLE user_notification_preferences
	DROP CONSTRAINT IF EXISTS user_notification_preferences_commit_author_success_source_check;

ALTER TABLE user_notification_preferences
	ADD CONSTRAINT user_notification_preferences_commit_author_success_source_check
	CHECK (commit_author_success_source IS NULL OR commit_author_success_source IN ('instance_default', 'user'));

-- +goose Down

ALTER TABLE user_notification_preferences
	DROP CONSTRAINT IF EXISTS user_notification_preferences_commit_author_success_source_check;

ALTER TABLE user_notification_preferences
	DROP COLUMN IF EXISTS commit_author_success_source;

ALTER TABLE user_notification_preferences
	DROP COLUMN IF EXISTS commit_author_success_enabled;

ALTER TABLE notification_instance_settings
	DROP COLUMN IF EXISTS default_commit_author_success_email_enabled;