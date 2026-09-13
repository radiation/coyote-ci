-- +goose Up

CREATE TABLE IF NOT EXISTS external_image_builds (
    execution_job_id UUID PRIMARY KEY REFERENCES build_jobs(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    submission_state TEXT NOT NULL,
    external_build_id TEXT,
    external_resource_name TEXT,
    source_bucket TEXT,
    source_object TEXT,
    source_generation TEXT,
    target_image_reference TEXT NOT NULL,
    submitted_at TIMESTAMPTZ,
    last_provider_status TEXT,
    terminal_result TEXT,
    image_digest TEXT,
    external_log_url TEXT,
    failure_detail TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (provider <> ''),
    CHECK (submission_state IN ('intent', 'staged', 'submitted', 'terminal')),
    CHECK (target_image_reference <> '')
);

CREATE INDEX IF NOT EXISTS idx_external_image_builds_recovery
    ON external_image_builds (submission_state, updated_at)
    WHERE submission_state <> 'terminal';

-- +goose Down

DROP TABLE IF EXISTS external_image_builds;