-- +goose Up
ALTER TABLE source_credentials
    ALTER COLUMN project_id DROP NOT NULL;

ALTER TABLE source_credentials
    DROP CONSTRAINT IF EXISTS source_credentials_project_id_name_key;

WITH ranked_source_credentials AS (
    SELECT
        id,
        name,
        project_id,
        ROW_NUMBER() OVER (
            PARTITION BY name
            ORDER BY created_at ASC, id ASC
        ) AS duplicate_rank
    FROM source_credentials
)
UPDATE source_credentials AS credentials
SET
    name = ranked.name || ' (' || ranked.project_id || ' ' || SUBSTRING(ranked.id::text, 1, 8) || ')',
    updated_at = NOW()
FROM ranked_source_credentials AS ranked
WHERE credentials.id = ranked.id
  AND ranked.duplicate_rank > 1;

ALTER TABLE source_credentials
    ADD CONSTRAINT source_credentials_name_key UNIQUE (name);

CREATE TABLE IF NOT EXISTS job_managed_image_configs (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    managed_image_name TEXT NOT NULL,
    pipeline_path TEXT NOT NULL,
    write_credential_id UUID NOT NULL REFERENCES source_credentials(id) ON DELETE RESTRICT,
    bot_branch_prefix TEXT NOT NULL DEFAULT 'coyote/managed-image-refresh',
    commit_author_name TEXT NOT NULL DEFAULT 'Coyote CI Bot',
    commit_author_email TEXT NOT NULL DEFAULT 'bot@coyote-ci.local',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (job_id)
);

CREATE INDEX IF NOT EXISTS idx_job_managed_image_configs_write_credential_id
    ON job_managed_image_configs (write_credential_id);

-- +goose Down
DROP INDEX IF EXISTS idx_job_managed_image_configs_write_credential_id;
DROP TABLE IF EXISTS job_managed_image_configs;

ALTER TABLE source_credentials
    DROP CONSTRAINT IF EXISTS source_credentials_name_key;

ALTER TABLE source_credentials
    ADD CONSTRAINT source_credentials_project_id_name_key UNIQUE (project_id, name);

ALTER TABLE source_credentials
    ALTER COLUMN project_id SET NOT NULL;