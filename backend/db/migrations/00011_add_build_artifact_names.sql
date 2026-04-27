-- +goose Up
ALTER TABLE build_artifacts
    ADD COLUMN IF NOT EXISTS artifact_name TEXT;

CREATE INDEX IF NOT EXISTS idx_build_artifacts_artifact_name_created_at
    ON build_artifacts (artifact_name, created_at DESC)
    WHERE artifact_name IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_build_artifacts_artifact_name_created_at;

ALTER TABLE build_artifacts
    DROP COLUMN IF EXISTS artifact_name;