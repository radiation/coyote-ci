-- +goose Up

CREATE TABLE IF NOT EXISTS slack_workspace_integrations (
	id UUID PRIMARY KEY,
	singleton BOOLEAN NOT NULL DEFAULT TRUE CHECK (singleton = TRUE),
	workspace_id TEXT NOT NULL,
	workspace_name TEXT,
	workspace_url TEXT,
	bot_user_id TEXT,
	authed_user_id TEXT,
	app_id TEXT,
	bot_token_secret TEXT NOT NULL,
	enabled BOOLEAN NOT NULL DEFAULT TRUE,
	connected_at TIMESTAMPTZ NOT NULL,
	last_tested_at TIMESTAMPTZ,
	last_test_succeeded BOOLEAN,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (singleton),
	UNIQUE (workspace_id)
);

-- +goose Down

DROP TABLE IF EXISTS slack_workspace_integrations;
