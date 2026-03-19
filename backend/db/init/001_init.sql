CREATE TABLE IF NOT EXISTS builds (
    id UUID PRIMARY KEY,
    project_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_builds_project_id ON builds (project_id);
CREATE INDEX IF NOT EXISTS idx_builds_created_at ON builds (created_at DESC);
