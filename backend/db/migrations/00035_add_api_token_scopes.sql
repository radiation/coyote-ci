-- +goose Up

ALTER TABLE api_tokens
ADD COLUMN IF NOT EXISTS scopes JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down

ALTER TABLE api_tokens
DROP COLUMN IF EXISTS scopes;