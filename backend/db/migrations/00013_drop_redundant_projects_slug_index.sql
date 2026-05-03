-- +goose Up

DROP INDEX IF EXISTS idx_projects_slug;

-- +goose Down

CREATE INDEX IF NOT EXISTS idx_projects_slug ON projects (slug);