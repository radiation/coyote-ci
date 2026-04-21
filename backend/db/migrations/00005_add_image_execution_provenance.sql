-- +goose Up
ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS requested_image_ref TEXT,
    ADD COLUMN IF NOT EXISTS resolved_image_ref TEXT,
    ADD COLUMN IF NOT EXISTS image_source_kind TEXT NOT NULL DEFAULT 'external',
    ADD COLUMN IF NOT EXISTS managed_image_id TEXT,
    ADD COLUMN IF NOT EXISTS managed_image_version_id TEXT;

ALTER TABLE build_steps
    ADD COLUMN IF NOT EXISTS requested_image_ref TEXT,
    ADD COLUMN IF NOT EXISTS resolved_image_ref TEXT,
    ADD COLUMN IF NOT EXISTS image_source_kind TEXT NOT NULL DEFAULT 'external',
    ADD COLUMN IF NOT EXISTS managed_image_id TEXT,
    ADD COLUMN IF NOT EXISTS managed_image_version_id TEXT;

ALTER TABLE builds
    DROP CONSTRAINT IF EXISTS builds_image_source_kind_check;

ALTER TABLE builds
    ADD CONSTRAINT builds_image_source_kind_check
    CHECK (image_source_kind IN ('external', 'managed'));

ALTER TABLE build_steps
    DROP CONSTRAINT IF EXISTS build_steps_image_source_kind_check;

ALTER TABLE build_steps
    ADD CONSTRAINT build_steps_image_source_kind_check
    CHECK (image_source_kind IN ('external', 'managed'));

CREATE INDEX IF NOT EXISTS idx_builds_managed_image_version_id
    ON builds (managed_image_version_id);

CREATE INDEX IF NOT EXISTS idx_build_steps_managed_image_version_id
    ON build_steps (managed_image_version_id);

CREATE TABLE IF NOT EXISTS managed_images (
    id UUID PRIMARY KEY,
    project_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS managed_image_versions (
    id UUID PRIMARY KEY,
    managed_image_id UUID NOT NULL REFERENCES managed_images(id) ON DELETE CASCADE,
    version_label TEXT NOT NULL,
    image_ref TEXT NOT NULL,
    image_digest TEXT NOT NULL,
    metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (managed_image_id, version_label),
    UNIQUE (managed_image_id, image_digest)
);

CREATE INDEX IF NOT EXISTS idx_managed_image_versions_image_digest
    ON managed_image_versions (image_digest);

-- +goose Down
DROP INDEX IF EXISTS idx_managed_image_versions_image_digest;
DROP TABLE IF EXISTS managed_image_versions;
DROP TABLE IF EXISTS managed_images;

DROP INDEX IF EXISTS idx_build_steps_managed_image_version_id;
DROP INDEX IF EXISTS idx_builds_managed_image_version_id;

ALTER TABLE build_steps
    DROP CONSTRAINT IF EXISTS build_steps_image_source_kind_check;

ALTER TABLE builds
    DROP CONSTRAINT IF EXISTS builds_image_source_kind_check;

ALTER TABLE build_steps
    DROP COLUMN IF EXISTS managed_image_version_id,
    DROP COLUMN IF EXISTS managed_image_id,
    DROP COLUMN IF EXISTS image_source_kind,
    DROP COLUMN IF EXISTS resolved_image_ref,
    DROP COLUMN IF EXISTS requested_image_ref;

ALTER TABLE builds
    DROP COLUMN IF EXISTS managed_image_version_id,
    DROP COLUMN IF EXISTS managed_image_id,
    DROP COLUMN IF EXISTS image_source_kind,
    DROP COLUMN IF EXISTS resolved_image_ref,
    DROP COLUMN IF EXISTS requested_image_ref;
