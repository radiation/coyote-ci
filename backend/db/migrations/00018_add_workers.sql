CREATE TABLE workers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    last_heartbeat_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX workers_last_heartbeat_idx ON workers (last_heartbeat_at DESC);