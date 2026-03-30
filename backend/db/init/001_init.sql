CREATE TABLE IF NOT EXISTS builds (
    id UUID PRIMARY KEY,
    project_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    queued_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    current_step_index INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    pipeline_config_yaml TEXT,
    pipeline_name TEXT,
    pipeline_source TEXT,
    repo_url TEXT,
    ref TEXT,
    commit_sha TEXT
);

CREATE INDEX IF NOT EXISTS idx_builds_project_id ON builds (project_id);
CREATE INDEX IF NOT EXISTS idx_builds_created_at ON builds (created_at DESC);

CREATE TABLE IF NOT EXISTS build_steps (
    id UUID PRIMARY KEY,
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    step_index INTEGER NOT NULL,
    name TEXT NOT NULL,
    command TEXT NOT NULL,
    args JSONB NOT NULL DEFAULT '[]'::jsonb,
    env JSONB NOT NULL DEFAULT '{}'::jsonb,
    working_dir TEXT NOT NULL DEFAULT '.',
    timeout_seconds INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    worker_id TEXT,
    claim_token TEXT,
    claimed_at TIMESTAMPTZ,
    lease_expires_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    exit_code INTEGER,
    stdout TEXT,
    stderr TEXT,
    error_message TEXT,
    UNIQUE (build_id, step_index)
);

CREATE INDEX IF NOT EXISTS idx_build_steps_build_id_step_index ON build_steps (build_id, step_index);

CREATE TABLE IF NOT EXISTS build_artifacts (
    id UUID PRIMARY KEY,
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    logical_path TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    content_type TEXT,
    checksum_sha256 TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (build_id, logical_path)
);

CREATE INDEX IF NOT EXISTS idx_build_artifacts_build_id_created_at
    ON build_artifacts (build_id, created_at);
