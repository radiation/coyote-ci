-- +goose Up

ALTER TABLE external_image_builds
    ADD COLUMN IF NOT EXISTS published_image_reference TEXT,
    ADD COLUMN IF NOT EXISTS published_image_digest_reference TEXT;

-- +goose Down

ALTER TABLE external_image_builds
    DROP COLUMN IF EXISTS published_image_digest_reference,
    DROP COLUMN IF EXISTS published_image_reference;
