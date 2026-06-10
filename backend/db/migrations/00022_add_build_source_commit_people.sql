-- +goose Up
ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS source_author_name TEXT,
    ADD COLUMN IF NOT EXISTS source_author_email TEXT,
    ADD COLUMN IF NOT EXISTS source_committer_name TEXT,
    ADD COLUMN IF NOT EXISTS source_committer_email TEXT;

-- +goose Down
ALTER TABLE builds
    DROP COLUMN IF EXISTS source_committer_email,
    DROP COLUMN IF EXISTS source_committer_name,
    DROP COLUMN IF EXISTS source_author_email,
    DROP COLUMN IF EXISTS source_author_name;