-- +goose Up
CREATE TABLE IF NOT EXISTS version_tags (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    version_text TEXT NOT NULL,
    target_type TEXT NOT NULL,
    artifact_id UUID REFERENCES build_artifacts(id) ON DELETE CASCADE,
    managed_image_version_id UUID REFERENCES managed_image_versions(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT version_tags_version_text_trimmed CHECK (btrim(version_text) <> ''),
    CONSTRAINT version_tags_target_type_valid CHECK (target_type IN ('artifact', 'managed_image_version')),
    CONSTRAINT version_tags_exactly_one_target CHECK (
        ((artifact_id IS NOT NULL)::int + (managed_image_version_id IS NOT NULL)::int) = 1
    ),
    CONSTRAINT version_tags_target_type_matches_target CHECK (
        (target_type = 'artifact' AND artifact_id IS NOT NULL AND managed_image_version_id IS NULL)
        OR
        (target_type = 'managed_image_version' AND artifact_id IS NULL AND managed_image_version_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS version_tags_artifact_unique_version_idx
    ON version_tags (job_id, artifact_id, version_text)
    WHERE artifact_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS version_tags_managed_image_version_unique_version_idx
    ON version_tags (job_id, managed_image_version_id, version_text)
    WHERE managed_image_version_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS version_tags_job_version_idx
    ON version_tags (job_id, version_text, created_at DESC);

CREATE INDEX IF NOT EXISTS version_tags_artifact_created_idx
    ON version_tags (artifact_id, created_at DESC)
    WHERE artifact_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS version_tags_managed_image_version_created_idx
    ON version_tags (managed_image_version_id, created_at DESC)
    WHERE managed_image_version_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS version_tags_managed_image_version_created_idx;
DROP INDEX IF EXISTS version_tags_artifact_created_idx;
DROP INDEX IF EXISTS version_tags_job_version_idx;
DROP INDEX IF EXISTS version_tags_managed_image_version_unique_version_idx;
DROP INDEX IF EXISTS version_tags_artifact_unique_version_idx;
DROP TABLE IF EXISTS version_tags;