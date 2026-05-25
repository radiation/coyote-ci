-- +goose Up

CREATE TABLE IF NOT EXISTS workers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    last_heartbeat_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS workers_last_heartbeat_idx ON workers (last_heartbeat_at DESC);

-- +goose Down

DROP INDEX IF EXISTS workers_last_heartbeat_idx;
DROP TABLE IF EXISTS workers;