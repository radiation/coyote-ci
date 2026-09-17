-- +goose Up

ALTER TABLE external_image_builds
    ADD COLUMN IF NOT EXISTS consumed_artifacts JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down

ALTER TABLE external_image_builds
    DROP COLUMN IF EXISTS consumed_artifacts;