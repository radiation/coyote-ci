-- +goose Up

ALTER TABLE jobs
	ADD COLUMN IF NOT EXISTS repository_id UUID REFERENCES scm_registered_repositories(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_jobs_repository_id ON jobs (repository_id);

-- +goose Down

DROP INDEX IF EXISTS idx_jobs_repository_id;

ALTER TABLE jobs
	DROP COLUMN IF EXISTS repository_id;