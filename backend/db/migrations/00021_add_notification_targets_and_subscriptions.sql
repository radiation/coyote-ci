-- +goose Up

CREATE TABLE IF NOT EXISTS notification_targets (
    id UUID PRIMARY KEY,
    type TEXT NOT NULL,
    name TEXT NOT NULL,
    recipient TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT notification_targets_type_check CHECK (type IN ('email')),
    CONSTRAINT notification_targets_type_recipient_key UNIQUE (type, recipient)
);

CREATE TABLE IF NOT EXISTS notification_subscriptions (
    id UUID PRIMARY KEY,
    target_id UUID NOT NULL REFERENCES notification_targets(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    job_id UUID REFERENCES jobs(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT notification_subscriptions_event_type_check CHECK (event_type IN ('build_succeeded', 'build_failed')),
    CONSTRAINT notification_subscriptions_scope_check CHECK (num_nonnulls(project_id, job_id) = 1)
);

CREATE UNIQUE INDEX IF NOT EXISTS notification_subscriptions_target_event_project_key
    ON notification_subscriptions (target_id, event_type, project_id)
    WHERE project_id IS NOT NULL AND job_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS notification_subscriptions_target_event_job_key
    ON notification_subscriptions (target_id, event_type, job_id)
    WHERE job_id IS NOT NULL AND project_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_notification_subscriptions_project_event_enabled
    ON notification_subscriptions (project_id, event_type, enabled)
    WHERE project_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_notification_subscriptions_job_event_enabled
    ON notification_subscriptions (job_id, event_type, enabled)
    WHERE job_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_notification_subscriptions_job_event_enabled;
DROP INDEX IF EXISTS idx_notification_subscriptions_project_event_enabled;
DROP INDEX IF EXISTS notification_subscriptions_target_event_job_key;
DROP INDEX IF EXISTS notification_subscriptions_target_event_project_key;
DROP TABLE IF EXISTS notification_subscriptions;
DROP TABLE IF EXISTS notification_targets;