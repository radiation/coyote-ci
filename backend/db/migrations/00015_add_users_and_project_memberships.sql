-- +goose Up

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    display_name TEXT,
    global_role TEXT NOT NULL DEFAULT 'user',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT users_global_role_check CHECK (global_role IN ('admin', 'user'))
);

CREATE TABLE IF NOT EXISTS project_memberships (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (project_id, user_id),
    CONSTRAINT project_memberships_role_check CHECK (role IN ('owner', 'maintainer', 'viewer'))
);

CREATE INDEX IF NOT EXISTS idx_project_memberships_user_id ON project_memberships (user_id);

-- +goose Down

DROP INDEX IF EXISTS idx_project_memberships_user_id;
DROP TABLE IF EXISTS project_memberships;
DROP TABLE IF EXISTS users;
