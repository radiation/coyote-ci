-- +goose Up

ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS is_public BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down

ALTER TABLE projects
    DROP COLUMN IF EXISTS is_public;