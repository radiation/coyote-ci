-- +goose Up

ALTER TABLE jobs
    ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 5;

ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 5;

UPDATE jobs
SET priority = 5
WHERE priority IS NULL;

UPDATE builds
SET priority = 5
WHERE priority IS NULL;

ALTER TABLE jobs
    DROP CONSTRAINT IF EXISTS chk_jobs_priority_range;

ALTER TABLE jobs
    ADD CONSTRAINT chk_jobs_priority_range
        CHECK (priority >= 1 AND priority <= 10);

ALTER TABLE builds
    DROP CONSTRAINT IF EXISTS chk_builds_priority_range;

ALTER TABLE builds
    ADD CONSTRAINT chk_builds_priority_range
        CHECK (priority >= 1 AND priority <= 10);

CREATE INDEX IF NOT EXISTS idx_builds_queue_priority
    ON builds (status, priority DESC, queued_at ASC, created_at ASC);

-- +goose Down

DROP INDEX IF EXISTS idx_builds_queue_priority;

ALTER TABLE builds
    DROP CONSTRAINT IF EXISTS chk_builds_priority_range;

ALTER TABLE jobs
    DROP CONSTRAINT IF EXISTS chk_jobs_priority_range;

ALTER TABLE builds
    DROP COLUMN IF EXISTS priority;

ALTER TABLE jobs
    DROP COLUMN IF EXISTS priority;