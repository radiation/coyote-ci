-- +goose Up

CREATE TABLE IF NOT EXISTS cache_entries (
    id UUID PRIMARY KEY,
    job_id TEXT NOT NULL,
    preset TEXT NOT NULL,
    cache_key TEXT NOT NULL,
    storage_provider TEXT NOT NULL,
    object_key TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    checksum TEXT,
    compression TEXT NOT NULL DEFAULT 'tar.gz',
    status TEXT NOT NULL,
    created_by_build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    created_by_step_id UUID NOT NULL REFERENCES build_steps(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_accessed_at TIMESTAMPTZ,
    UNIQUE (job_id, preset, cache_key)
);

CREATE INDEX IF NOT EXISTS idx_cache_entries_lookup
    ON cache_entries (job_id, preset, cache_key);

CREATE INDEX IF NOT EXISTS idx_cache_entries_ready_lookup
    ON cache_entries (job_id, preset, status);

-- +goose Down

DROP TABLE IF EXISTS cache_entries;
