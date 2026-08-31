-- +goose Up

CREATE TABLE IF NOT EXISTS workspace_revisions (
    id UUID PRIMARY KEY,
    producing_execution_job_id UUID NOT NULL REFERENCES build_jobs(id) ON DELETE CASCADE,
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL,
    attempt_number INTEGER NOT NULL,
    parent_revision_id UUID REFERENCES workspace_revisions(id) ON DELETE SET NULL,
    status TEXT NOT NULL,
    content_digest TEXT,
    storage_key TEXT,
    size_bytes BIGINT,
    created_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    UNIQUE (producing_execution_job_id),
    CHECK (status IN ('publishing', 'published', 'deleted')),
    CHECK (attempt_number >= 1),
    CHECK (size_bytes IS NULL OR size_bytes >= 0),
    CONSTRAINT workspace_revisions_state_check CHECK (
        (status = 'publishing'
            AND content_digest IS NULL
            AND storage_key IS NULL
            AND size_bytes IS NULL
            AND published_at IS NULL
            AND deleted_at IS NULL)
        OR (status = 'published'
            AND content_digest IS NOT NULL
            AND storage_key IS NOT NULL
            AND published_at IS NOT NULL
            AND deleted_at IS NULL)
        OR (status = 'deleted'
            AND content_digest IS NOT NULL
            AND storage_key IS NOT NULL
            AND published_at IS NOT NULL
            AND deleted_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_workspace_revisions_published_build_node
    ON workspace_revisions (build_id, node_id, attempt_number DESC, created_at DESC)
    WHERE status = 'published';

-- +goose Down

DROP TABLE IF EXISTS workspace_revisions;