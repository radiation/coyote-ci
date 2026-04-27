-- +goose Up
ALTER TABLE build_artifacts
    ADD COLUMN IF NOT EXISTS artifact_type TEXT;

UPDATE build_artifacts
SET artifact_type = CASE
    WHEN artifact_type IS NOT NULL THEN artifact_type
    WHEN content_type ILIKE '%docker%' OR content_type ILIKE '%oci%' THEN 'docker_image'
    WHEN logical_path ILIKE '%.oci' THEN 'docker_image'
    WHEN logical_path ILIKE '%.tar' AND (
        logical_path ILIKE '%docker%'
        OR logical_path ILIKE '%image%'
        OR logical_path ILIKE '%container%'
    ) THEN 'docker_image'
    WHEN logical_path ILIKE '%.tgz' THEN 'npm_package'
    WHEN COALESCE(content_type, '') = '' AND split_part(reverse(logical_path), '/', 1) NOT LIKE '%.%' THEN 'unknown'
    ELSE 'generic'
END
WHERE artifact_type IS NULL;

ALTER TABLE build_artifacts
    ADD CONSTRAINT build_artifacts_artifact_type_valid
    CHECK (artifact_type IS NULL OR artifact_type IN ('docker_image', 'npm_package', 'generic', 'unknown'));

CREATE INDEX IF NOT EXISTS idx_build_artifacts_artifact_type_created_at
    ON build_artifacts (artifact_type, created_at DESC)
    WHERE artifact_type IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_build_artifacts_artifact_type_created_at;

ALTER TABLE build_artifacts
    DROP CONSTRAINT IF EXISTS build_artifacts_artifact_type_valid;

ALTER TABLE build_artifacts
    DROP COLUMN IF EXISTS artifact_type;