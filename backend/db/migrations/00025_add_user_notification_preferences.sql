-- +goose Up

CREATE TABLE IF NOT EXISTS user_notification_preferences (
	user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	commit_author_failure_enabled BOOLEAN NOT NULL DEFAULT FALSE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down

DROP TABLE IF EXISTS user_notification_preferences;