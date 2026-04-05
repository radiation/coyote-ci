CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY,
    project_id TEXT NOT NULL,
    name TEXT NOT NULL,
    repository_url TEXT NOT NULL,
    default_ref TEXT,
    default_commit_sha TEXT,
    push_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    push_branch TEXT,
    trigger_mode TEXT NOT NULL DEFAULT 'branches',
    branch_allowlist JSONB NOT NULL DEFAULT '[]'::jsonb,
    tag_allowlist JSONB NOT NULL DEFAULT '[]'::jsonb,
    pipeline_yaml TEXT,
    pipeline_path TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_jobs_project_id ON jobs (project_id);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs (created_at DESC);

CREATE TABLE IF NOT EXISTS builds (
    id UUID PRIMARY KEY,
    project_id TEXT NOT NULL,
    job_id UUID REFERENCES jobs(id) ON DELETE SET NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    queued_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    current_step_index INTEGER NOT NULL DEFAULT 0,
    attempt_number INTEGER NOT NULL DEFAULT 1,
    rerun_of_build_id UUID REFERENCES builds(id) ON DELETE SET NULL,
    rerun_from_step_index INTEGER,
    error_message TEXT,
    pipeline_config_yaml TEXT,
    pipeline_name TEXT,
    pipeline_source TEXT,
    pipeline_path TEXT,
    repo_url TEXT,
    ref TEXT,
    commit_sha TEXT,
    trigger_kind TEXT NOT NULL DEFAULT 'manual',
    scm_provider TEXT,
    event_type TEXT,
    trigger_repository_owner TEXT,
    trigger_repository_name TEXT,
    trigger_repository_url TEXT,
    trigger_raw_ref TEXT,
    trigger_ref TEXT,
    trigger_ref_type TEXT,
    trigger_ref_name TEXT,
    trigger_deleted BOOLEAN,
    trigger_commit_sha TEXT,
    trigger_delivery_id TEXT,
    trigger_actor TEXT
);

CREATE INDEX IF NOT EXISTS idx_builds_project_id ON builds (project_id);
CREATE INDEX IF NOT EXISTS idx_builds_job_id ON builds (job_id);
CREATE INDEX IF NOT EXISTS idx_builds_created_at ON builds (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_builds_rerun_of_build_id ON builds (rerun_of_build_id);

CREATE TABLE IF NOT EXISTS build_steps (
    id UUID PRIMARY KEY,
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    step_index INTEGER NOT NULL,
    name TEXT NOT NULL,
    image TEXT NOT NULL DEFAULT '',
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
    artifact_paths JSONB NOT NULL DEFAULT '[]'::jsonb,
    UNIQUE (build_id, step_index)
);

CREATE INDEX IF NOT EXISTS idx_build_steps_build_id_step_index ON build_steps (build_id, step_index);

CREATE TABLE IF NOT EXISTS build_jobs (
    id UUID PRIMARY KEY,
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    step_id UUID NOT NULL REFERENCES build_steps(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    step_index INTEGER NOT NULL,
    attempt_number INTEGER NOT NULL DEFAULT 1,
    retry_of_job_id UUID REFERENCES build_jobs(id) ON DELETE SET NULL,
    lineage_root_job_id UUID REFERENCES build_jobs(id) ON DELETE SET NULL,
    status TEXT NOT NULL,
    queue_name TEXT,
    image TEXT NOT NULL,
    working_dir TEXT NOT NULL,
    command_json JSONB NOT NULL,
    env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    timeout_seconds INTEGER,
    pipeline_file_path TEXT,
    context_dir TEXT,
    source_repo_url TEXT,
    source_commit_sha TEXT NOT NULL,
    source_ref_name TEXT,
    source_archive_uri TEXT,
    source_archive_digest TEXT,
    spec_version INTEGER NOT NULL DEFAULT 1,
    spec_digest TEXT,
    resolved_spec_json JSONB NOT NULL,
    claim_token TEXT,
    claimed_by TEXT,
    claim_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error_message TEXT,
    exit_code INTEGER,
    output_refs_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    UNIQUE (build_id, step_id, attempt_number)
);

CREATE INDEX IF NOT EXISTS idx_build_jobs_build_id ON build_jobs (build_id);
CREATE INDEX IF NOT EXISTS idx_build_jobs_status_created_at ON build_jobs (status, created_at);
CREATE INDEX IF NOT EXISTS idx_build_jobs_claim_expires_at ON build_jobs (claim_expires_at);
CREATE INDEX IF NOT EXISTS idx_build_jobs_step_latest ON build_jobs (step_id, attempt_number DESC, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_build_jobs_retry_of_job_id ON build_jobs (retry_of_job_id);
CREATE INDEX IF NOT EXISTS idx_build_jobs_lineage_root_job_id ON build_jobs (lineage_root_job_id);

CREATE TABLE IF NOT EXISTS build_job_outputs (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES build_jobs(id) ON DELETE CASCADE,
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    declared_path TEXT NOT NULL,
    destination_uri TEXT,
    content_type TEXT,
    size_bytes BIGINT,
    digest TEXT,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_build_job_outputs_build_id ON build_job_outputs (build_id, created_at);
CREATE INDEX IF NOT EXISTS idx_build_job_outputs_job_id ON build_job_outputs (job_id, created_at);

CREATE TABLE IF NOT EXISTS build_artifacts (
    id UUID PRIMARY KEY,
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    step_id UUID REFERENCES build_steps(id) ON DELETE SET NULL,
    logical_path TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    storage_provider TEXT NOT NULL DEFAULT 'filesystem',
    size_bytes BIGINT NOT NULL,
    content_type TEXT,
    checksum_sha256 TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (build_id, logical_path)
);

CREATE INDEX IF NOT EXISTS idx_build_artifacts_build_id_created_at
    ON build_artifacts (build_id, created_at);

CREATE TABLE IF NOT EXISTS build_step_logs (
    id BIGSERIAL PRIMARY KEY,
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    step_id UUID NOT NULL REFERENCES build_steps(id) ON DELETE CASCADE,
    step_index INTEGER NOT NULL,
    step_name TEXT NOT NULL,
    sequence_no BIGINT GENERATED ALWAYS AS IDENTITY,
    stream TEXT NOT NULL,
    chunk_text TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (build_id, step_index, sequence_no)
);

CREATE INDEX IF NOT EXISTS idx_build_step_logs_step_sequence
    ON build_step_logs (build_id, step_index, sequence_no);

CREATE INDEX IF NOT EXISTS idx_build_step_logs_build
    ON build_step_logs (build_id, sequence_no);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY,
    provider TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    event_type TEXT,
    repository_owner TEXT,
    repository_name TEXT,
    trigger_raw_ref TEXT,
    trigger_ref_type TEXT,
    trigger_ref_name TEXT,
    trigger_ref TEXT,
    trigger_deleted BOOLEAN,
    commit_sha TEXT,
    actor TEXT,
    status TEXT NOT NULL,
    matched_job_id UUID REFERENCES jobs(id) ON DELETE SET NULL,
    queued_build_id UUID REFERENCES builds(id) ON DELETE SET NULL,
    reason TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, delivery_id)
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_provider_received_at
    ON webhook_deliveries (provider, received_at DESC);
