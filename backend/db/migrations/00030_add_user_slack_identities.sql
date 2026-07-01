-- +goose Up

CREATE TABLE IF NOT EXISTS user_slack_identities (
	id UUID PRIMARY KEY,
	user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	slack_workspace_integration_id UUID NOT NULL REFERENCES slack_workspace_integrations(id) ON DELETE RESTRICT,
	slack_user_id TEXT NOT NULL,
	slack_display_name TEXT,
	slack_real_name TEXT,
	slack_handle TEXT,
	slack_email TEXT,
	profile_image_url TEXT,
	enabled BOOLEAN NOT NULL DEFAULT TRUE,
	linked_at TIMESTAMPTZ NOT NULL,
	last_verified_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT user_slack_identities_user_id_key UNIQUE (user_id),
	CONSTRAINT user_slack_identities_workspace_user_key UNIQUE (slack_workspace_integration_id, slack_user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_slack_identities_workspace_integration_id
	ON user_slack_identities (slack_workspace_integration_id);

-- +goose Down

DROP INDEX IF EXISTS idx_user_slack_identities_workspace_integration_id;
DROP TABLE IF EXISTS user_slack_identities;