-- +goose Up
ALTER TABLE jobs
    ADD COLUMN pull_request_enabled BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE jobs
    DROP COLUMN pull_request_enabled;