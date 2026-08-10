-- +goose Up
ALTER TABLE builds
    ADD COLUMN pull_request_number BIGINT,
    ADD COLUMN pull_request_action TEXT,
    ADD COLUMN pull_request_url TEXT,
    ADD COLUMN pull_request_base_ref TEXT,
    ADD COLUMN pull_request_base_sha TEXT,
    ADD COLUMN pull_request_head_ref TEXT,
    ADD COLUMN pull_request_head_sha TEXT,
    ADD COLUMN pull_request_source_mode TEXT,
    ADD CONSTRAINT builds_pull_request_snapshot_check CHECK (
        (pull_request_number IS NULL
            AND pull_request_action IS NULL
            AND pull_request_url IS NULL
            AND pull_request_base_ref IS NULL
            AND pull_request_base_sha IS NULL
            AND pull_request_head_ref IS NULL
            AND pull_request_head_sha IS NULL
            AND pull_request_source_mode IS NULL)
        OR
        (pull_request_number IS NOT NULL
            AND pull_request_action IS NOT NULL
            AND pull_request_url IS NOT NULL
            AND pull_request_base_ref IS NOT NULL
            AND pull_request_base_sha IS NOT NULL
            AND pull_request_head_ref IS NOT NULL
            AND pull_request_head_sha IS NOT NULL
            AND pull_request_source_mode IS NOT NULL
            AND pull_request_number > 0
            AND pull_request_action IN ('opened', 'reopened', 'synchronize')
            AND btrim(pull_request_url) <> ''
            AND btrim(pull_request_base_ref) <> ''
            AND btrim(pull_request_base_sha) <> ''
            AND btrim(pull_request_head_ref) <> ''
            AND btrim(pull_request_head_sha) <> ''
            AND pull_request_source_mode = 'head')
    );

-- +goose Down
ALTER TABLE builds
    DROP CONSTRAINT builds_pull_request_snapshot_check,
    DROP COLUMN pull_request_source_mode,
    DROP COLUMN pull_request_head_sha,
    DROP COLUMN pull_request_head_ref,
    DROP COLUMN pull_request_base_sha,
    DROP COLUMN pull_request_base_ref,
    DROP COLUMN pull_request_url,
    DROP COLUMN pull_request_action,
    DROP COLUMN pull_request_number;