-- +goose Up

UPDATE workspace_revisions
SET storage_provider = NULL
WHERE status = 'publishing';

ALTER TABLE workspace_revisions
    DROP CONSTRAINT workspace_revisions_storage_provider_check,
    ALTER COLUMN storage_provider DROP NOT NULL,
    DROP CONSTRAINT workspace_revisions_state_check,
    ADD CONSTRAINT workspace_revisions_state_check CHECK (
        (status = 'publishing'
            AND content_digest IS NULL
            AND storage_key IS NULL
            AND storage_provider IS NULL
            AND size_bytes IS NULL
            AND published_at IS NULL
            AND deleted_at IS NULL)
        OR (status = 'published'
            AND content_digest IS NOT NULL
            AND storage_key IS NOT NULL
            AND storage_provider IS NOT NULL
            AND storage_provider IN ('filesystem', 'gcs')
            AND published_at IS NOT NULL
            AND deleted_at IS NULL)
        OR (status = 'deleted'
            AND content_digest IS NOT NULL
            AND storage_key IS NOT NULL
            AND storage_provider IS NOT NULL
            AND storage_provider IN ('filesystem', 'gcs')
            AND published_at IS NOT NULL
            AND deleted_at IS NOT NULL)
    );

-- +goose Down

UPDATE workspace_revisions
SET storage_provider = 'filesystem'
WHERE storage_provider IS NULL;

ALTER TABLE workspace_revisions
    DROP CONSTRAINT workspace_revisions_state_check,
    ALTER COLUMN storage_provider SET NOT NULL,
    ADD CONSTRAINT workspace_revisions_storage_provider_check
        CHECK (storage_provider IN ('filesystem', 'gcs')),
    ADD CONSTRAINT workspace_revisions_state_check CHECK (
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
    );
