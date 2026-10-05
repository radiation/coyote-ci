-- +goose Up

ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS application_version TEXT;

ALTER TABLE builds
    ADD CONSTRAINT builds_application_version_nonblank
    CHECK (application_version IS NULL OR btrim(application_version) <> '');

-- +goose Down

ALTER TABLE builds
    DROP CONSTRAINT IF EXISTS builds_application_version_nonblank;

ALTER TABLE builds
    DROP COLUMN IF EXISTS application_version;
