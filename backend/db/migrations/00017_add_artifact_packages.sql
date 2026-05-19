-- +goose Up

CREATE TABLE IF NOT EXISTS artifact_packages (
    id UUID PRIMARY KEY,
    project_id TEXT NOT NULL,
    job_id UUID REFERENCES jobs(id) ON DELETE CASCADE,
    scope_build_id UUID REFERENCES builds(id) ON DELETE CASCADE,
    logical_path TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT artifact_packages_scope_check CHECK (
        (job_id IS NOT NULL AND scope_build_id IS NULL)
        OR (job_id IS NULL AND scope_build_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS artifact_packages_job_logical_path_idx
    ON artifact_packages (job_id, logical_path)
    WHERE job_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS artifact_packages_build_logical_path_idx
    ON artifact_packages (scope_build_id, logical_path)
    WHERE job_id IS NULL;

CREATE INDEX IF NOT EXISTS artifact_packages_project_idx
    ON artifact_packages (project_id, created_at DESC);

ALTER TABLE build_artifacts
    ADD COLUMN IF NOT EXISTS package_id UUID;

WITH scoped_artifacts AS (
    SELECT
        a.id AS artifact_id,
        b.project_id::text AS project_id,
        b.job_id,
        CASE WHEN b.job_id IS NULL THEN b.id ELSE NULL END AS scope_build_id,
        a.logical_path,
        MIN(a.created_at) OVER (
            PARTITION BY COALESCE(b.job_id::text, b.id::text), a.logical_path
        ) AS package_created_at,
        md5(COALESCE(b.job_id::text, b.id::text) || '::' || a.logical_path) AS package_hash
    FROM build_artifacts a
    JOIN builds b ON b.id = a.build_id
), distinct_packages AS (
    SELECT DISTINCT ON (package_hash)
        (
            substr(package_hash, 1, 8) || '-' ||
            substr(package_hash, 9, 4) || '-' ||
            '4' || substr(package_hash, 14, 3) || '-' ||
            'a' || substr(package_hash, 18, 3) || '-' ||
            substr(package_hash, 21, 12)
        )::uuid AS id,
        project_id,
        job_id,
        scope_build_id,
        logical_path,
        package_created_at AS created_at,
        package_hash
    FROM scoped_artifacts
    ORDER BY package_hash, package_created_at ASC
)
INSERT INTO artifact_packages (id, project_id, job_id, scope_build_id, logical_path, created_at)
SELECT id, project_id, job_id, scope_build_id, logical_path, created_at
FROM distinct_packages
ON CONFLICT DO NOTHING;

WITH scoped_artifacts AS (
    SELECT
        a.id AS artifact_id,
        (
            substr(md5(COALESCE(b.job_id::text, b.id::text) || '::' || a.logical_path), 1, 8) || '-' ||
            substr(md5(COALESCE(b.job_id::text, b.id::text) || '::' || a.logical_path), 9, 4) || '-' ||
            '4' || substr(md5(COALESCE(b.job_id::text, b.id::text) || '::' || a.logical_path), 14, 3) || '-' ||
            'a' || substr(md5(COALESCE(b.job_id::text, b.id::text) || '::' || a.logical_path), 18, 3) || '-' ||
            substr(md5(COALESCE(b.job_id::text, b.id::text) || '::' || a.logical_path), 21, 12)
        )::uuid AS package_id
    FROM build_artifacts a
    JOIN builds b ON b.id = a.build_id
)
UPDATE build_artifacts a
SET package_id = scoped_artifacts.package_id
FROM scoped_artifacts
WHERE a.id = scoped_artifacts.artifact_id
  AND a.package_id IS NULL;

ALTER TABLE build_artifacts
    ALTER COLUMN package_id SET NOT NULL;

ALTER TABLE build_artifacts
    ADD CONSTRAINT build_artifacts_package_id_fkey
        FOREIGN KEY (package_id) REFERENCES artifact_packages(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_build_artifacts_package_id_created_at
    ON build_artifacts (package_id, created_at DESC);

CREATE TABLE IF NOT EXISTS artifact_versions (
    id UUID PRIMARY KEY,
    package_id UUID NOT NULL REFERENCES artifact_packages(id) ON DELETE CASCADE,
    artifact_id UUID NOT NULL REFERENCES build_artifacts(id) ON DELETE CASCADE,
    version_text TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT artifact_versions_version_text_trimmed CHECK (btrim(version_text) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS artifact_versions_package_version_idx
    ON artifact_versions (package_id, version_text);

CREATE INDEX IF NOT EXISTS artifact_versions_artifact_created_idx
    ON artifact_versions (artifact_id, created_at DESC);

CREATE INDEX IF NOT EXISTS artifact_versions_package_created_idx
    ON artifact_versions (package_id, created_at DESC);

CREATE TABLE IF NOT EXISTS artifact_channels (
    id UUID PRIMARY KEY,
    package_id UUID NOT NULL REFERENCES artifact_packages(id) ON DELETE CASCADE,
    channel_name TEXT NOT NULL,
    current_artifact_id UUID NOT NULL REFERENCES build_artifacts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT artifact_channels_channel_name_trimmed CHECK (btrim(channel_name) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS artifact_channels_package_channel_idx
    ON artifact_channels (package_id, channel_name);

CREATE INDEX IF NOT EXISTS artifact_channels_artifact_updated_idx
    ON artifact_channels (current_artifact_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS artifact_channel_events (
    id UUID PRIMARY KEY,
    package_id UUID NOT NULL REFERENCES artifact_packages(id) ON DELETE CASCADE,
    channel_name TEXT NOT NULL,
    previous_artifact_id UUID REFERENCES build_artifacts(id) ON DELETE SET NULL,
    new_artifact_id UUID NOT NULL REFERENCES build_artifacts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT artifact_channel_events_channel_name_trimmed CHECK (btrim(channel_name) <> '')
);

CREATE INDEX IF NOT EXISTS artifact_channel_events_package_channel_created_idx
    ON artifact_channel_events (package_id, channel_name, created_at DESC);

INSERT INTO artifact_versions (id, package_id, artifact_id, version_text, created_at)
SELECT DISTINCT ON (a.package_id, vt.version_text)
    vt.id,
    a.package_id,
    a.id,
    vt.version_text,
    vt.created_at
FROM version_tags vt
JOIN build_artifacts a ON a.id = vt.artifact_id
WHERE vt.target_type = 'artifact'
ORDER BY a.package_id, vt.version_text, vt.created_at DESC, a.created_at DESC, a.id DESC
ON CONFLICT (package_id, version_text) DO NOTHING;

-- +goose Down

DELETE FROM artifact_versions
WHERE id IN (
    SELECT vt.id
    FROM version_tags vt
    WHERE vt.target_type = 'artifact'
);

DROP INDEX IF EXISTS artifact_channel_events_package_channel_created_idx;
DROP TABLE IF EXISTS artifact_channel_events;

DROP INDEX IF EXISTS artifact_channels_artifact_updated_idx;
DROP INDEX IF EXISTS artifact_channels_package_channel_idx;
DROP TABLE IF EXISTS artifact_channels;

DROP INDEX IF EXISTS artifact_versions_package_created_idx;
DROP INDEX IF EXISTS artifact_versions_artifact_created_idx;
DROP INDEX IF EXISTS artifact_versions_package_version_idx;
DROP TABLE IF EXISTS artifact_versions;

DROP INDEX IF EXISTS idx_build_artifacts_package_id_created_at;
ALTER TABLE build_artifacts DROP CONSTRAINT IF EXISTS build_artifacts_package_id_fkey;
ALTER TABLE build_artifacts DROP COLUMN IF EXISTS package_id;

DROP INDEX IF EXISTS artifact_packages_project_idx;
DROP INDEX IF EXISTS artifact_packages_build_logical_path_idx;
DROP INDEX IF EXISTS artifact_packages_job_logical_path_idx;
DROP TABLE IF EXISTS artifact_packages;
