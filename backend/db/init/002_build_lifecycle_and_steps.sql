ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS queued_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS current_step_index INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS error_message TEXT;

CREATE TABLE IF NOT EXISTS build_steps (
    id UUID PRIMARY KEY,
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    step_index INTEGER NOT NULL,
    name TEXT NOT NULL,
    command TEXT NOT NULL DEFAULT 'sh',
    args JSONB NOT NULL DEFAULT '[]'::jsonb,
    env JSONB NOT NULL DEFAULT '{}'::jsonb,
    working_dir TEXT NOT NULL DEFAULT '.',
    timeout_seconds INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    worker_id TEXT,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    exit_code INTEGER,
    error_message TEXT,
    UNIQUE (build_id, step_index)
);

CREATE INDEX IF NOT EXISTS idx_build_steps_build_id_step_index ON build_steps (build_id, step_index);
