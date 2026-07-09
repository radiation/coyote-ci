-- +goose Up

ALTER TABLE builds
ALTER COLUMN trigger_producer_project_id TYPE UUID USING NULLIF(btrim(trigger_producer_project_id), '')::uuid,
ALTER COLUMN trigger_producer_job_id TYPE UUID USING NULLIF(btrim(trigger_producer_job_id), '')::uuid;

-- +goose Down

ALTER TABLE builds
ALTER COLUMN trigger_producer_job_id TYPE TEXT USING trigger_producer_job_id::text,
ALTER COLUMN trigger_producer_project_id TYPE TEXT USING trigger_producer_project_id::text;