-- +goose Up

CREATE TABLE application_releases (
    id UUID PRIMARY KEY,
    build_id UUID NOT NULL UNIQUE REFERENCES builds(id) ON DELETE RESTRICT,
    project_id TEXT NOT NULL,
    job_id UUID REFERENCES jobs(id) ON DELETE RESTRICT,
    application_version TEXT NOT NULL CHECK (btrim(application_version) <> ''),
    status TEXT NOT NULL CHECK (status IN ('pending', 'complete', 'failed')),
    auto_channel_name TEXT,
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX application_releases_job_version_unique
    ON application_releases (project_id, job_id, application_version) WHERE job_id IS NOT NULL;
CREATE INDEX application_releases_pending_idx ON application_releases (created_at) WHERE status = 'pending';

CREATE TABLE application_release_outputs (
    id UUID PRIMARY KEY,
    release_id UUID NOT NULL REFERENCES application_releases(id) ON DELETE CASCADE,
    output_kind TEXT NOT NULL CHECK (output_kind IN ('artifact', 'image')),
    output_key TEXT NOT NULL,
    display_name TEXT NOT NULL,
    definition_json JSONB NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'published', 'failed')),
    artifact_id UUID REFERENCES build_artifacts(id) ON DELETE RESTRICT,
    artifact_version_id UUID REFERENCES artifact_versions(id) ON DELETE RESTRICT,
    execution_job_id UUID REFERENCES build_jobs(id) ON DELETE RESTRICT,
    immutable_reference TEXT,
    failure_reason TEXT,
    published_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (release_id, output_key)
);

CREATE TABLE application_channels (
    id UUID PRIMARY KEY,
    project_id TEXT NOT NULL,
    job_id UUID REFERENCES jobs(id) ON DELETE RESTRICT,
    channel_name TEXT NOT NULL CHECK (channel_name IN ('latest', 'stable')),
    current_release_id UUID NOT NULL REFERENCES application_releases(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX application_channels_job_name_unique
    ON application_channels (project_id, job_id, channel_name) WHERE job_id IS NOT NULL;
CREATE TABLE application_channel_events (
    id UUID PRIMARY KEY,
    channel_id UUID NOT NULL REFERENCES application_channels(id) ON DELETE RESTRICT,
    previous_release_id UUID REFERENCES application_releases(id) ON DELETE RESTRICT,
    new_release_id UUID NOT NULL REFERENCES application_releases(id) ON DELETE RESTRICT,
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX application_channel_events_channel_created_idx ON application_channel_events (channel_id, created_at DESC);

-- +goose Down
DROP TABLE application_channel_events;
DROP TABLE application_channels;
DROP TABLE application_release_outputs;
DROP TABLE application_releases;
