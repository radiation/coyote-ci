-- +goose Up
CREATE TABLE IF NOT EXISTS source_credentials (
    id UUID PRIMARY KEY,
    project_id TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    username TEXT,
    secret_ref TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name),
    CONSTRAINT source_credentials_kind_check CHECK (kind IN ('https_token', 'ssh_key'))
);

CREATE TABLE IF NOT EXISTS repo_writeback_configs (
    id UUID PRIMARY KEY,
    project_id TEXT NOT NULL,
    repository_url TEXT NOT NULL,
    pipeline_path TEXT NOT NULL DEFAULT '.coyote/pipeline.yml',
    managed_image_name TEXT NOT NULL,
    write_credential_id UUID NOT NULL REFERENCES source_credentials(id) ON DELETE RESTRICT,
    bot_branch_prefix TEXT NOT NULL DEFAULT 'coyote/managed-image-refresh',
    commit_author_name TEXT NOT NULL DEFAULT 'Coyote CI Bot',
    commit_author_email TEXT NOT NULL DEFAULT 'bot@coyote-ci.local',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, repository_url)
);

ALTER TABLE managed_image_versions
    ADD COLUMN IF NOT EXISTS dependency_fingerprint TEXT,
    ADD COLUMN IF NOT EXISTS source_repository_url TEXT;

CREATE INDEX IF NOT EXISTS idx_managed_image_versions_dependency_fingerprint
    ON managed_image_versions (managed_image_id, dependency_fingerprint);

-- +goose Down
DROP INDEX IF EXISTS idx_managed_image_versions_dependency_fingerprint;

ALTER TABLE managed_image_versions
    DROP COLUMN IF EXISTS source_repository_url,
    DROP COLUMN IF EXISTS dependency_fingerprint;

DROP TABLE IF EXISTS repo_writeback_configs;
DROP TABLE IF EXISTS source_credentials;
