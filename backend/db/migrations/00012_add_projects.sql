-- +goose Up

CREATE TABLE IF NOT EXISTS projects (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

INSERT INTO projects (id, name, slug, description, created_at, updated_at)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    'Default Project',
    'default',
    'Compatibility project for jobs created before project assignment existed.',
    NOW(),
    NOW()
)
ON CONFLICT (slug) DO NOTHING;

UPDATE jobs
SET project_id = '00000000-0000-0000-0000-000000000001';

ALTER TABLE jobs
    ALTER COLUMN project_id TYPE UUID USING project_id::uuid;

ALTER TABLE jobs
    ALTER COLUMN project_id SET NOT NULL;

ALTER TABLE jobs
    ADD CONSTRAINT fk_jobs_project_id
        FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_projects_slug ON projects (slug);

UPDATE builds
SET project_id = '00000000-0000-0000-0000-000000000001'
WHERE TRIM(COALESCE(project_id, '')) = '';

UPDATE builds b
SET project_id = j.project_id::text
FROM jobs j
WHERE b.job_id = j.id;

-- +goose Down

ALTER TABLE jobs
    DROP CONSTRAINT IF EXISTS fk_jobs_project_id;

ALTER TABLE jobs
    ALTER COLUMN project_id TYPE TEXT USING project_id::text;

DROP INDEX IF EXISTS idx_projects_slug;

DELETE FROM projects
WHERE id = '00000000-0000-0000-0000-000000000001';

DROP TABLE IF EXISTS projects;