-- +goose Up

ALTER TABLE jobs
ADD COLUMN IF NOT EXISTS artifact_triggers JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE builds
ADD COLUMN IF NOT EXISTS trigger_producer_project_id TEXT,
ADD COLUMN IF NOT EXISTS trigger_producer_job_id TEXT,
ADD COLUMN IF NOT EXISTS trigger_producer_build_id UUID,
ADD COLUMN IF NOT EXISTS trigger_artifact_id UUID,
ADD COLUMN IF NOT EXISTS trigger_artifact_path TEXT,
ADD COLUMN IF NOT EXISTS trigger_artifact_name TEXT,
ADD COLUMN IF NOT EXISTS trigger_artifact_size_bytes BIGINT,
ADD COLUMN IF NOT EXISTS trigger_artifact_checksum_sha256 TEXT;

CREATE TABLE IF NOT EXISTS artifact_trigger_deliveries (
	id UUID PRIMARY KEY,
	artifact_id UUID NOT NULL REFERENCES build_artifacts(id) ON DELETE CASCADE,
	consumer_job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
	producer_build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
	producer_project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	producer_job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
	artifact_path TEXT NOT NULL,
	queued_build_id UUID REFERENCES builds(id) ON DELETE SET NULL,
	error_message TEXT,
	status TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (artifact_id, consumer_job_id)
);

CREATE INDEX IF NOT EXISTS idx_artifact_trigger_deliveries_consumer_job_id
	ON artifact_trigger_deliveries (consumer_job_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_artifact_trigger_deliveries_producer_build_id
	ON artifact_trigger_deliveries (producer_build_id, created_at DESC);

-- +goose Down

DROP INDEX IF EXISTS idx_artifact_trigger_deliveries_producer_build_id;

DROP INDEX IF EXISTS idx_artifact_trigger_deliveries_consumer_job_id;

DROP TABLE IF EXISTS artifact_trigger_deliveries;

ALTER TABLE builds
DROP COLUMN IF EXISTS trigger_artifact_checksum_sha256,
DROP COLUMN IF EXISTS trigger_artifact_size_bytes,
DROP COLUMN IF EXISTS trigger_artifact_name,
DROP COLUMN IF EXISTS trigger_artifact_path,
DROP COLUMN IF EXISTS trigger_artifact_id,
DROP COLUMN IF EXISTS trigger_producer_build_id,
DROP COLUMN IF EXISTS trigger_producer_job_id,
DROP COLUMN IF EXISTS trigger_producer_project_id;

ALTER TABLE jobs
DROP COLUMN IF EXISTS artifact_triggers;