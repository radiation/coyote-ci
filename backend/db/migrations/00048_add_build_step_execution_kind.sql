-- +goose Up

ALTER TABLE build_steps
    ADD COLUMN IF NOT EXISTS execution_kind TEXT NOT NULL DEFAULT 'shell',
    ADD COLUMN IF NOT EXISTS remote_image_build JSONB;

-- +goose Down

ALTER TABLE build_steps
    DROP COLUMN IF EXISTS remote_image_build,
    DROP COLUMN IF EXISTS execution_kind;