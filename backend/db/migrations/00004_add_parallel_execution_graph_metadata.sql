-- +goose Up

ALTER TABLE build_steps
    ADD COLUMN IF NOT EXISTS node_id TEXT,
    ADD COLUMN IF NOT EXISTS group_name TEXT,
    ADD COLUMN IF NOT EXISTS depends_on_node_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE build_jobs
    ADD COLUMN IF NOT EXISTS node_id TEXT,
    ADD COLUMN IF NOT EXISTS group_name TEXT,
    ADD COLUMN IF NOT EXISTS depends_on_node_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE build_steps
SET node_id = CONCAT('node-', LPAD(step_index::text, 3, '0'))
WHERE node_id IS NULL OR BTRIM(node_id) = '';

UPDATE build_jobs
SET node_id = CONCAT('node-', LPAD(step_index::text, 3, '0'))
WHERE node_id IS NULL OR BTRIM(node_id) = '';

CREATE INDEX IF NOT EXISTS idx_build_steps_build_node_id ON build_steps (build_id, node_id);
CREATE INDEX IF NOT EXISTS idx_build_jobs_build_node_id ON build_jobs (build_id, node_id);

-- +goose Down

DROP INDEX IF EXISTS idx_build_jobs_build_node_id;
DROP INDEX IF EXISTS idx_build_steps_build_node_id;

ALTER TABLE build_jobs
    DROP COLUMN IF EXISTS depends_on_node_ids,
    DROP COLUMN IF EXISTS group_name,
    DROP COLUMN IF EXISTS node_id;

ALTER TABLE build_steps
    DROP COLUMN IF EXISTS depends_on_node_ids,
    DROP COLUMN IF EXISTS group_name,
    DROP COLUMN IF EXISTS node_id;