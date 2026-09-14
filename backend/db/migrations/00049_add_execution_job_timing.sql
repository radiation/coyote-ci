-- +goose Up

CREATE TABLE IF NOT EXISTS execution_job_timings (
	execution_job_id UUID PRIMARY KEY REFERENCES build_jobs(id) ON DELETE CASCADE,
	timing_json JSONB NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down

DROP TABLE IF EXISTS execution_job_timings;