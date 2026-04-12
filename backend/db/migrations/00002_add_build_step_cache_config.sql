-- +goose Up

ALTER TABLE build_steps
    ADD COLUMN IF NOT EXISTS cache_config JSONB;

-- +goose Down

ALTER TABLE build_steps
    DROP COLUMN IF EXISTS cache_config;