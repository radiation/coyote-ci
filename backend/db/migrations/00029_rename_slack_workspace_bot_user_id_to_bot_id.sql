-- +goose Up

ALTER TABLE slack_workspace_integrations
	RENAME COLUMN bot_user_id TO bot_id;

-- +goose Down

ALTER TABLE slack_workspace_integrations
	RENAME COLUMN bot_id TO bot_user_id;