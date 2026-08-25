-- +goose Up

ALTER TABLE build_jobs
    ADD COLUMN IF NOT EXISTS failure_kind TEXT;

-- +goose Down

ALTER TABLE build_jobs
    DROP COLUMN IF EXISTS failure_kind;
