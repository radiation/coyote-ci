-- +goose Up

ALTER TABLE workspace_revisions
    ADD COLUMN storage_provider TEXT;

UPDATE workspace_revisions
SET storage_provider = 'filesystem'
WHERE storage_provider IS NULL;

ALTER TABLE workspace_revisions
    ALTER COLUMN storage_provider SET NOT NULL,
    ADD CONSTRAINT workspace_revisions_storage_provider_check
        CHECK (storage_provider IN ('filesystem', 'gcs'));

-- +goose Down

ALTER TABLE workspace_revisions
    DROP CONSTRAINT workspace_revisions_storage_provider_check,
    DROP COLUMN storage_provider;
