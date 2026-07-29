-- +goose Up

ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS registered_repository_id UUID REFERENCES scm_registered_repositories(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS scm_connection_id UUID REFERENCES scm_connections(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS provider_repository_id TEXT,
    ADD CONSTRAINT builds_repository_identity_snapshot_check CHECK (
        (registered_repository_id IS NULL AND scm_connection_id IS NULL AND provider_repository_id IS NULL)
        OR
        (registered_repository_id IS NOT NULL AND scm_connection_id IS NOT NULL AND provider_repository_id IS NOT NULL AND provider_repository_id <> '')
    );

ALTER TABLE scm_status_deliveries
    ADD COLUMN IF NOT EXISTS registered_repository_id UUID REFERENCES scm_registered_repositories(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS scm_connection_id UUID REFERENCES scm_connections(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS provider_repository_id TEXT,
    ADD CONSTRAINT scm_status_deliveries_repository_identity_snapshot_check CHECK (
        (registered_repository_id IS NULL AND scm_connection_id IS NULL AND provider_repository_id IS NULL)
        OR
        (registered_repository_id IS NOT NULL AND scm_connection_id IS NOT NULL AND provider_repository_id IS NOT NULL AND provider_repository_id <> '')
    );

ALTER TABLE scm_status_deliveries
    DROP CONSTRAINT IF EXISTS scm_status_deliveries_stream_key_unique;

CREATE UNIQUE INDEX IF NOT EXISTS scm_status_deliveries_repository_stream_key_unique
    ON scm_status_deliveries (scm_connection_id, provider_repository_id, commit_sha, context_name)
    WHERE scm_connection_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS scm_status_deliveries_legacy_stream_key_unique
    ON scm_status_deliveries (provider, repository_owner, repository_name, commit_sha, context_name)
    WHERE scm_connection_id IS NULL;

-- +goose Down

DROP INDEX IF EXISTS scm_status_deliveries_legacy_stream_key_unique;
DROP INDEX IF EXISTS scm_status_deliveries_repository_stream_key_unique;

-- Rollback can fail if separate connections now share a legacy owner/name/SHA/context tuple.
-- This migration intentionally does not remove, merge, or rewrite those durable delivery rows.
ALTER TABLE scm_status_deliveries
    ADD CONSTRAINT scm_status_deliveries_stream_key_unique UNIQUE (provider, repository_owner, repository_name, commit_sha, context_name);

ALTER TABLE scm_status_deliveries
    DROP CONSTRAINT IF EXISTS scm_status_deliveries_repository_identity_snapshot_check,
    DROP COLUMN IF EXISTS provider_repository_id,
    DROP COLUMN IF EXISTS scm_connection_id,
    DROP COLUMN IF EXISTS registered_repository_id;

ALTER TABLE builds
    DROP CONSTRAINT IF EXISTS builds_repository_identity_snapshot_check,
    DROP COLUMN IF EXISTS provider_repository_id,
    DROP COLUMN IF EXISTS scm_connection_id,
    DROP COLUMN IF EXISTS registered_repository_id;