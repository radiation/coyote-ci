-- +goose Up
ALTER TABLE build_artifacts
    ADD COLUMN IF NOT EXISTS target_platform TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE build_artifacts
    DROP COLUMN IF EXISTS target_platform;