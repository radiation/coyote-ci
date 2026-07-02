-- +goose Up

ALTER TABLE user_notification_preferences
	ADD COLUMN IF NOT EXISTS commit_author_failure_email_enabled BOOLEAN,
	ADD COLUMN IF NOT EXISTS commit_author_failure_slack_enabled BOOLEAN NOT NULL DEFAULT FALSE,
	ADD COLUMN IF NOT EXISTS commit_author_failure_email_source TEXT,
	ADD COLUMN IF NOT EXISTS commit_author_success_email_enabled BOOLEAN,
	ADD COLUMN IF NOT EXISTS commit_author_success_slack_enabled BOOLEAN NOT NULL DEFAULT FALSE,
	ADD COLUMN IF NOT EXISTS commit_author_success_email_source TEXT;

UPDATE user_notification_preferences
SET
	commit_author_failure_email_enabled = commit_author_failure_enabled,
	commit_author_failure_email_source = source,
	commit_author_success_email_enabled = commit_author_success_enabled,
	commit_author_success_email_source = commit_author_success_source
WHERE commit_author_failure_email_enabled IS NULL
	OR commit_author_failure_email_source IS NULL
	OR commit_author_success_email_enabled IS NULL;

UPDATE user_notification_preferences
SET commit_author_failure_email_source = 'user'
WHERE commit_author_failure_email_source IS NULL;

ALTER TABLE user_notification_preferences
	ALTER COLUMN commit_author_failure_email_enabled SET NOT NULL,
	ALTER COLUMN commit_author_failure_email_source SET NOT NULL,
	ALTER COLUMN commit_author_success_email_enabled SET NOT NULL;

ALTER TABLE user_notification_preferences
	DROP CONSTRAINT IF EXISTS user_notification_preferences_source_check,
	DROP CONSTRAINT IF EXISTS user_notification_preferences_commit_author_success_source_check;

ALTER TABLE user_notification_preferences
	ADD CONSTRAINT user_notification_preferences_commit_author_failure_email_source_check
		CHECK (commit_author_failure_email_source IN ('instance_default', 'user')),
	ADD CONSTRAINT user_notification_preferences_commit_author_success_email_source_check
		CHECK (commit_author_success_email_source IS NULL OR commit_author_success_email_source IN ('instance_default', 'user'));

ALTER TABLE user_notification_preferences
	DROP COLUMN IF EXISTS commit_author_failure_enabled,
	DROP COLUMN IF EXISTS source,
	DROP COLUMN IF EXISTS commit_author_success_enabled,
	DROP COLUMN IF EXISTS commit_author_success_source;

-- +goose Down

ALTER TABLE user_notification_preferences
	ADD COLUMN IF NOT EXISTS commit_author_failure_enabled BOOLEAN NOT NULL DEFAULT FALSE,
	ADD COLUMN IF NOT EXISTS source TEXT,
	ADD COLUMN IF NOT EXISTS commit_author_success_enabled BOOLEAN NOT NULL DEFAULT FALSE,
	ADD COLUMN IF NOT EXISTS commit_author_success_source TEXT;

UPDATE user_notification_preferences
SET
	commit_author_failure_enabled = commit_author_failure_email_enabled,
	source = commit_author_failure_email_source,
	commit_author_success_enabled = commit_author_success_email_enabled,
	commit_author_success_source = commit_author_success_email_source;

UPDATE user_notification_preferences
SET source = 'user'
WHERE source IS NULL;

ALTER TABLE user_notification_preferences
	ALTER COLUMN source SET NOT NULL;

ALTER TABLE user_notification_preferences
	DROP CONSTRAINT IF EXISTS user_notification_preferences_commit_author_failure_email_source_check,
	DROP CONSTRAINT IF EXISTS user_notification_preferences_commit_author_success_email_source_check;

ALTER TABLE user_notification_preferences
	ADD CONSTRAINT user_notification_preferences_source_check
		CHECK (source IN ('instance_default', 'user')),
	ADD CONSTRAINT user_notification_preferences_commit_author_success_source_check
		CHECK (commit_author_success_source IS NULL OR commit_author_success_source IN ('instance_default', 'user'));

ALTER TABLE user_notification_preferences
	DROP COLUMN IF EXISTS commit_author_failure_email_enabled,
	DROP COLUMN IF EXISTS commit_author_failure_slack_enabled,
	DROP COLUMN IF EXISTS commit_author_failure_email_source,
	DROP COLUMN IF EXISTS commit_author_success_email_enabled,
	DROP COLUMN IF EXISTS commit_author_success_slack_enabled,
	DROP COLUMN IF EXISTS commit_author_success_email_source;